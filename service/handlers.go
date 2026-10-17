package service

import (
	"errors"
	"io"
	"net/http"
	"os"
	"time"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/api"
	"github.com/superdb/super/api/queryio"
	"github.com/superdb/super/compiler"
	"github.com/superdb/super/compiler/describe"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/db"
	dbapi "github.com/superdb/super/db/api"
	"github.com/superdb/super/db/commits"
	"github.com/superdb/super/db/journal"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/service/auth"
	"github.com/superdb/super/service/srverr"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/anyio"
	"github.com/superdb/super/sio/csvio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
	"go.uber.org/zap"
)

func handleQuery(c *Core, w *ResponseWriter, r *Request) {
	const queryStatsInterval = time.Second
	var req api.QueryRequest
	if !r.Unmarshal(w, &req) {
		return
	}
	r.Logger.Debug("Running Query", zap.String("query", req.Query))
	ctrl, ok := r.BoolFromQuery(w, "ctrl")
	if !ok {
		return
	}
	// A note on error handling here.  If we get an error setting up
	// before the query starts to run, we call w.Error() and return
	// an HTTP status error and a JSON formatted error.  If the query
	// begins running then we encounter an error, we return an HTTP
	// status OK (triggered as we start to write to the HTTP response body)
	// and return the error as an embedded BSUP control message.
	// The client must look at the return code and interpret the result
	// accordingly and when it sees a BSUP error after underway,
	// the error should be relay that to the caller/user.
	ast, err := parser.ParseText(req.Query)
	if err != nil {
		w.Error(srverr.ErrInvalid(err))
		return
	}
	sctx := super.NewContext()
	flowgraph, err := runtime.CompileQueryForDB(r.Context(), sctx, c.compiler, ast)
	if err != nil {
		w.Error(srverr.ErrInvalid(err))
		return
	}
	flusher, _ := w.ResponseWriter.(http.Flusher)
	writer, err := queryio.NewWriter(sctx, sio.NopCloser(w), w.Format, flusher, ctrl)
	if err != nil {
		w.Error(srverr.ErrInvalid(err))
		return
	}
	// Once we defer writer.Close() are going to write BSUP to the HTTP
	// response body and for errors after this point, we must call
	// writer.WriterError() instead of w.Error().
	defer writer.Close()
	// Launch query status which will report and runtime errors (i.e., system
	// errors that occur after the OK header has been sent) to the query status
	// endpoint.
	status := c.newQueryStatus(r)
	defer status.Done()
	handleError := func(err error) {
		writer.WriteError(err)
		status.setError(err)
	}
	type result struct {
		vec vector.Any
		err error
	}
	resultCh := make(chan result)
	go func() {
		for {
			vec, err := flowgraph.Pull(false)
			resultCh <- result{vec, err}
			if vec == nil || err != nil {
				return
			}
		}
	}()
	timer := time.NewTicker(queryStatsInterval)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			if err := writer.WriteProgress(flowgraph.Progress()); err != nil {
				w.Logger.Warn("Error writing progress to client", zap.Error(err))
				handleError(err)
				return
			}
		case r := <-resultCh:
			vec, err := r.vec, r.err
			if err != nil {
				if !errors.Is(err, journal.ErrEmpty) {
					w.Logger.Warn("Error from query", zap.Error(err))
					handleError(err)
				}
				return
			}
			var label string
			if labeled, ok := vec.(*vector.Labeled); ok {
				label = labeled.Label
				vec = labeled.Any
				// A label with a null vector signals end of output channel.
				if vec == nil {
					if err := writer.WriteChannelEnd(label); err != nil {
						w.Logger.Warn("Error writing channel end to client", zap.Error(err))
						handleError(err)
						return
					}
					continue
				}
			}
			if vec == nil {
				// query is done
				if err := writer.WriteProgress(flowgraph.Progress()); err != nil {
					w.Logger.Warn("Error writing progress to client", zap.Error(err))
					handleError(err)
				}
				return
			}
			if err := writer.Push(label, vec); err != nil {
				w.Logger.Warn("Error writing data to client", zap.Error(err))
				handleError(err)
				return
			}
		}
	}
}

func handleQueryStatus(c *Core, w *ResponseWriter, r *Request) {
	id, ok := r.StringFromPath(w, "requestID")
	if !ok {
		return
	}
	c.runningQueriesMu.Lock()
	q, ok := c.runningQueries[id]
	c.runningQueriesMu.Unlock()
	if !ok {
		w.Error(srverr.ErrInvalid("query not found"))
		return
	}
	q.wg.Wait()
	w.Respond(http.StatusOK, api.QueryError{Error: q.error})
}

func handleCompile(c *Core, w *ResponseWriter, r *Request) {
	var req api.QueryRequest
	if !r.Unmarshal(w, &req) {
		return
	}
	ast, err := parser.ParseText(req.Query)
	if err != nil {
		w.Error(srverr.ErrInvalid(err))
		return
	}
	w.Respond(http.StatusOK, ast.Parsed())
}

func handleQueryDescribe(c *Core, w *ResponseWriter, r *Request) {
	var req api.QueryRequest
	if !r.Unmarshal(w, &req) {
		return
	}
	env := exec.NewEnvironment(storage.NewRemoteEngine(), c.root)
	info, err := describe.Analyze(r.Context(), req.Query, env)
	if err != nil {
		w.Error(srverr.ErrInvalid(err))
		return
	}
	w.Respond(http.StatusOK, info)
}

func handleBranchGet(c *Core, w *ResponseWriter, r *Request) {
	branchName, ok := r.StringFromPath(w, "branch")
	if !ok {
		return
	}
	table, ok := r.openTable(w, c.root)
	if !ok {
		return
	}
	if branchName != "" {
		branch, err := table.LookupBranchByName(r.Context(), branchName)
		if err != nil {
			w.Error(err)
			return
		}
		w.Respond(http.StatusOK, api.CommitResponse{Commit: branch.Commit})
		return
	}
	w.Respond(http.StatusOK, table.Config)
}

func handleTableStats(c *Core, w *ResponseWriter, r *Request) {
	table, ok := r.openTable(w, c.root)
	if !ok {
		return
	}
	//XXX app uses this for key range... should handle this differently
	// at minimum on a per-branch basis and app needs to be branch aware
	// If branch not specified, API endpoints here should just assume "main".
	branch, err := table.OpenBranchByName(r.Context(), "main")
	if err != nil {
		w.Error(err)
		return
	}
	snap, err := branch.Table().Snapshot(r.Context(), branch.Commit)
	if err != nil {
		if errors.Is(err, journal.ErrEmpty) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Error(err)
		return
	}
	info, err := exec.GetTableStats(r.Context(), table, snap)
	if err != nil {
		w.Error(err)
		return
	}
	w.Respond(http.StatusOK, info)
}

func handleTablePost(c *Core, w *ResponseWriter, r *Request) {
	var req api.TablePostRequest
	if !r.Unmarshal(w, &req) {
		return
	}
	table, err := c.root.CreateTable(r.Context(), req.Name, req.SortKeys, req.ObjectCap, req.FrameCap)
	if err != nil {
		w.Error(err)
		return
	}
	meta, err := table.Main(r.Context())
	if err != nil {
		w.Error(err)
		return
	}
	w.Respond(http.StatusOK, meta)
	c.publishEvent(w, "table-new", api.EventTable{TableID: table.ID})
}

func handleTablePut(c *Core, w *ResponseWriter, r *Request) {
	var req api.TablePutRequest
	if !r.Unmarshal(w, &req) {
		return
	}
	id, ok := r.TableID(w, c.root)
	if !ok {
		return
	}
	if err := c.root.RenameTable(r.Context(), id, req.Name); err != nil {
		w.Error(err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	c.publishEvent(w, "table-update", api.EventTable{TableID: id})
}

func handleBranchPost(c *Core, w *ResponseWriter, r *Request) {
	var req api.BranchPostRequest
	if !r.Unmarshal(w, &req) {
		return
	}
	tableID, ok := r.TableID(w, c.root)
	if !ok {
		return
	}
	commit, err := dbid.ParseID(req.Commit)
	if err != nil {
		w.Error(srverr.ErrInvalid("invalid commit object: %s", req.Commit))
		return
	}
	branchRef, err := c.root.CreateBranch(r.Context(), tableID, req.Name, commit)
	if err != nil {
		w.Error(err)
		return
	}
	w.Respond(http.StatusOK, branchRef)
	c.publishEvent(w, "branch-update", api.EventBranch{TableID: tableID, Branch: branchRef.Name})
}

func handleRevertPost(c *Core, w *ResponseWriter, r *Request) {
	tableID, ok := r.TableID(w, c.root)
	if !ok {
		return
	}
	branch, ok := r.StringFromPath(w, "branch")
	if !ok {
		return
	}
	commit, ok := r.CommitID(w)
	if !ok {
		return
	}
	message, ok := r.decodeCommitMessage(w)
	if !ok {
		return
	}
	commit, err := c.root.Revert(r.Context(), tableID, branch, commit, message.Author, message.Body)
	if err != nil {
		w.Error(err)
		return
	}
	w.Respond(http.StatusOK, api.CommitResponse{Commit: commit})
	c.publishEvent(w, "branch-commit", api.EventBranchCommit{
		CommitID: commit,
		TableID:  tableID,
		Branch:   branch,
	})
}

func handleBranchMerge(c *Core, w *ResponseWriter, r *Request) {
	tableID, ok := r.TableID(w, c.root)
	if !ok {
		return
	}
	parentBranch, ok := r.StringFromPath(w, "branch")
	if !ok {
		return
	}
	childBranch, ok := r.StringFromPath(w, "child")
	if !ok {
		return
	}
	message, ok := r.decodeCommitMessage(w)
	if !ok {
		return
	}
	commit, err := c.root.MergeBranch(r.Context(), tableID, childBranch, parentBranch, message.Author, message.Body)
	if err != nil {
		w.Error(err)
		return
	}
	w.Respond(http.StatusOK, api.CommitResponse{Commit: commit})
	c.publishEvent(w, "branch-commit", api.EventBranchCommit{
		CommitID: commit,
		TableID:  tableID,
		Branch:   childBranch,
		Parent:   parentBranch,
	})
}

func handleTableDelete(c *Core, w *ResponseWriter, r *Request) {
	id, ok := r.TableID(w, c.root)
	if !ok {
		return
	}
	if err := c.root.RemoveTable(r.Context(), id); err != nil {
		w.Error(err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	c.publishEvent(w, "table-delete", api.EventTable{TableID: id})
}

func handleBranchDelete(c *Core, w *ResponseWriter, r *Request) {
	tableID, ok := r.TableID(w, c.root)
	if !ok {
		return
	}
	branchName, ok := r.StringFromPath(w, "branch")
	if !ok {
		return
	}
	if err := c.root.RemoveBranch(r.Context(), tableID, branchName); err != nil {
		w.Error(err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	c.publishEvent(w, "branch-delete", api.EventBranch{TableID: tableID, Branch: branchName})
}

func handleBranchLoad(c *Core, w *ResponseWriter, r *Request) {
	branchName, ok := r.StringFromPath(w, "branch")
	if !ok {
		return
	}
	format, ok := r.format(w, "auto")
	if !ok {
		return
	}
	var csvDelim rune
	if s := r.URL.Query().Get("csv.delim"); s != "" {
		if len(s) != 1 {
			w.Error(srverr.ErrInvalid(`invalid query param "csv.delim": must be exactly one character`))
			return
		}
		csvDelim = rune(s[0])
	}
	message, ok := r.decodeCommitMessage(w)
	if !ok {
		return
	}
	table, ok := r.openTable(w, c.root)
	if !ok {
		return
	}
	branch, err := table.OpenBranchByName(r.Context(), branchName)
	if err != nil {
		w.Error(err)
		return
	}
	reader, err := anyio.GzipReader(r.Body)
	if err != nil {
		w.Error(err)
		return
	}
	if format == "parquet" {
		// These formats require a reader that implements io.ReaderAt and
		// io.Seeker.  Copy the reader to a temporary file and use that.
		//
		// TODO: Add a way to disable this or limit file size.
		f, err := os.CreateTemp("", "super-db-load-")
		if err != nil {
			w.Error(err)
			return
		}
		defer f.Close()
		defer os.Remove(f.Name())
		if _, err := io.Copy(f, reader); err != nil {
			w.Error(err)
			return
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			w.Error(err)
			return
		}
		reader = f
	}
	opts := anyio.ReaderOpts{
		Format: format,
		CSV:    csvio.ReaderOpts{Delim: csvDelim},
		// Force validation of BSUP when loading into the database.
		//BSUP: bsupio.ReaderOpts{Validate: true},
	}
	sctx := super.NewContext()
	p, err := anyio.NewReader(r.Context(), sctx, reader, opts)
	if err != nil {
		w.Error(srverr.ErrInvalid(err))
		return
	}
	defer p.Pull(true)
	// XXX Load should handle vectors natively.
	wr := &warningsReader{p, []string{}}
	kommit, err := branch.Load(r.Context(), sctx, wr, message.Author, message.Body, message.Meta)
	if err != nil {
		if errors.Is(err, commits.ErrEmptyTransaction) {
			err = srverr.ErrInvalid("no records in request")
		}
		if errors.Is(err, db.ErrInvalidCommitMeta) {
			err = srverr.ErrInvalid("invalid commit metadata in request")
		}
		w.Error(err)
		return
	}
	w.Respond(http.StatusOK, api.CommitResponse{
		Warnings: wr.warnings,
		Commit:   kommit,
	})
	c.publishEvent(w, "branch-commit", api.EventBranchCommit{
		CommitID: kommit,
		TableID:  table.ID,
		Branch:   branch.Name,
	})
}

type warningsReader struct {
	vio.Puller
	warnings []string
}

func (w *warningsReader) Pull(done bool) (vector.Any, error) {
	vec, err := w.Puller.Pull(done)
	if err != nil {
		w.warnings = append(w.warnings, err.Error())
		return nil, nil
	}
	return vec, nil
}

func handleCompact(c *Core, w *ResponseWriter, r *Request) {
	var req api.CompactRequest
	if !r.Unmarshal(w, &req) {
		return
	}
	branchName, ok := r.StringFromPath(w, "branch")
	if !ok {
		return
	}
	message, ok := r.decodeCommitMessage(w)
	if !ok {
		return
	}
	table, ok := r.openTable(w, c.root)
	if !ok {
		return
	}
	ctx := r.Context()
	branch, err := table.OpenBranchByName(ctx, branchName)
	if err != nil {
		w.Error(err)
		return
	}
	commit, err := branch.Compact(ctx, c.compiler, req.ObjectIDs, message.Author, message.Body, message.Meta)
	if err != nil {
		w.Error(err)
		return
	}
	w.Respond(http.StatusOK, api.CommitResponse{Commit: commit})
	c.publishEvent(w, "branch-commit", api.EventBranchCommit{
		CommitID: commit,
		TableID:  table.ID,
		Branch:   branchName,
	})
	w.Respond(http.StatusOK, api.CommitResponse{Commit: branch.Commit})
	return
}

func handleDelete(c *Core, w *ResponseWriter, r *Request) {
	branchName, ok := r.StringFromPath(w, "branch")
	if !ok {
		return
	}
	message, ok := r.decodeCommitMessage(w)
	if !ok {
		return
	}
	var payload api.DeleteRequest
	if !r.Unmarshal(w, &payload) {
		return
	}
	table, ok := r.openTable(w, c.root)
	if !ok {
		return
	}
	branch, err := table.OpenBranchByName(r.Context(), branchName)
	if err != nil {
		w.Error(err)
		return
	}
	var commit uuid.UUID
	if len(payload.ObjectIDs) > 0 {
		if payload.Where != "" {
			w.Error(srverr.ErrInvalid("object_ids and where cannot both be set"))
			return
		}
		var ids []uuid.UUID
		ids, err = dbid.ParseIDs(payload.ObjectIDs)
		if err != nil {
			w.Error(srverr.ErrInvalid(err))
			return
		}
		commit, err = branch.Delete(r.Context(), ids, message.Author, message.Body)
	} else {
		if payload.Where == "" {
			w.Error(srverr.ErrInvalid("either object_ids or where must be set"))
			return
		}
		ast, err2 := parser.ParseText(payload.Where)
		if err2 != nil {
			w.Error(srverr.ErrInvalid(err2))
			return
		}
		commit, err = branch.DeleteWhere(r.Context(), c.compiler, ast, message.Author, message.Body, message.Meta)
		if errors.Is(err, commits.ErrEmptyTransaction) ||
			errors.Is(err, &compiler.InvalidDeleteWhereQuery{}) {
			err = srverr.ErrInvalid(err)
		}
	}
	if err != nil {
		w.Error(err)
		return
	}
	w.Marshal(api.CommitResponse{Commit: commit})
	c.publishEvent(w, "branch-commit", api.EventBranchCommit{
		CommitID: commit,
		TableID:  table.ID,
		Branch:   branchName,
	})
}

func handleVacate(c *Core, w *ResponseWriter, r *Request) {
	table, ok := r.StringFromPath(w, "table")
	if !ok {
		return
	}
	dryrun, ok := r.BoolFromQuery(w, "dryrun")
	if !ok {
		return
	}
	s := r.URL.Query().Get("ts")
	if s == "" {
		w.Error(srverr.ErrInvalid("missing required query param: ts"))
		return
	}
	ts, err := nano.ParseRFC3339Nano([]byte(s))
	if err != nil {
		w.Error(srverr.ErrInvalid("invalid timestamp value %q: %w", s, err))
		return
	}
	db := dbapi.FromRoot(c.root)
	cids, err := db.Vacate(r.Context(), table, ts, dryrun)
	if err != nil {
		w.Error(err)
		return
	}
	w.Respond(http.StatusOK, api.VacateResponse{CommitIDs: cids})
}

func handleVacuum(c *Core, w *ResponseWriter, r *Request) {
	table, ok := r.StringFromPath(w, "table")
	if !ok {
		return
	}
	revision, ok := r.StringFromPath(w, "revision")
	if !ok {
		return
	}
	dryrun, ok := r.BoolFromQuery(w, "dryrun")
	if !ok {
		return
	}
	db := dbapi.FromRoot(c.root)
	oids, err := db.Vacuum(r.Context(), table, revision, dryrun)
	if err != nil {
		w.Error(err)
		return
	}
	w.Respond(http.StatusOK, api.VacuumResponse{ObjectIDs: oids})
}

func handleAuthIdentityGet(c *Core, w *ResponseWriter, r *Request) {
	ident := auth.IdentityFromContext(r.Context())
	w.Respond(http.StatusOK, api.AuthIdentityResponse{
		TenantID: string(ident.TenantID),
		UserID:   string(ident.UserID),
	})
}

func handleAuthMethodGet(c *Core, w *ResponseWriter, r *Request) {
	if c.auth == nil {
		w.Respond(http.StatusOK, api.AuthMethodResponse{Kind: api.AuthMethodNone})
		return
	}
	w.Respond(http.StatusOK, c.auth.MethodResponse())
}

func handleEvents(c *Core, w *ResponseWriter, r *Request) {
	format, err := api.MediaTypeToFormat(r.Header.Get("Accept"), "sup")
	if err != nil {
		w.Error(srverr.ErrInvalid(err))
	}
	writer := &eventStreamWriter{body: w.ResponseWriter, format: format}
	subscription := make(chan event)
	c.subscriptionsMu.Lock()
	c.subscriptions[subscription] = struct{}{}
	c.subscriptionsMu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	// Flush header to notify clients that the request has been accepted.
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
	for {
		select {
		case ev := <-subscription:
			if err := writer.writeEvent(ev); err != nil {
				w.Error(err)
				continue
			}
			if f, ok := w.ResponseWriter.(http.Flusher); ok {
				f.Flush()
			}
		case <-r.Context().Done():
			c.subscriptionsMu.Lock()
			delete(c.subscriptions, subscription)
			c.subscriptionsMu.Unlock()
			return
		}
	}
}
