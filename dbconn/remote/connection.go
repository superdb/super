// Package remote implements dbconn.Conn for a database served by the
// SuperDB service (see the service package).
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/db"
	"github.com/superdb/super/db/branches"
	"github.com/superdb/super/dbconn"
	"github.com/superdb/super/dbconn/remote/auth0"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/service/api"
	"github.com/superdb/super/service/api/queryio"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/bsupio"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector/vio"
)

const (
	// DefaultPort port to connect to.
	DefaultPort      = 9867
	DefaultUserAgent = "super-client-golang"
)

var (
	// ErrTableNotFound is returned when the specified table does not exist.
	ErrTableNotFound = errors.New("table not found")
	// ErrTableExists is returned when the specified the table already exists.
	ErrTableExists = errors.New("table exists")
	// ErrBranchNotFound is returned when the specified branch does not exist.
	ErrBranchNotFound = errors.New("branch not found")
	// ErrBranchExists is returned when the specified the branch already exists.
	ErrBranchExists = errors.New("branch exists")
)

type Conn struct {
	auth          *auth0.Store
	client        *http.Client
	defaultHeader http.Header
	hostURL       string
}

var _ dbconn.Conn = (*Conn)(nil)

// NewConn creates a new connection with the given useragent string
// and a base URL derived from the hostURL argument.
func NewConn(hostURL string) *Conn {
	defaultHeader := http.Header{
		"Accept":       []string{api.MediaTypeBSUP},
		"Content-Type": []string{api.MediaTypeBSUP},
	}
	return &Conn{
		client:        &http.Client{},
		defaultHeader: defaultHeader,
		hostURL:       strings.TrimRight(hostURL, "/"),
	}
}

// ClientHostURL allows us to print the host in log messages and internal error messages
func (c *Conn) ClientHostURL() string {
	return c.hostURL
}

func (c *Conn) SetAuthToken(token string) {
	c.defaultHeader.Set("Authorization", "Bearer "+token)
}

func (c *Conn) SetAuthStore(store *auth0.Store) error {
	tokens, err := store.Tokens(c.hostURL)
	if err != nil || tokens == nil {
		return err
	}
	c.auth = store
	c.SetAuthToken(tokens.Access)
	return nil
}

func (c *Conn) SetUserAgent(useragent string) {
	c.defaultHeader.Set("User-Agent", useragent)
}

type Response struct {
	*http.Response
	Duration time.Duration
}

// Do sends an HTTP request and returns an HTTP response, refreshing the auth
// token if necessary.
//
// As for net/http.Client.Do, if the returned error is nil, the user is expected
// to call Response.Body.Close.
func (c *Conn) Do(req *Request) (*Response, error) {
	for i := 0; ; i++ {
		httpreq, err := req.HTTPRequest()
		if err != nil {
			return nil, err
		}
		res, err := c.client.Do(httpreq)
		if err != nil {
			return nil, err
		}
		if res.StatusCode < 200 || res.StatusCode > 299 {
			// parseError calls res.Body.Close.
			err = parseError(res)
			var reserr *ErrorResponse
			if i == 0 && res.StatusCode == 401 && errors.As(err, &reserr) && reserr.Err.Error() == "invalid token" {
				access, err := c.refreshAuthToken(req.ctx)
				if err != nil {
					return nil, err
				}
				req.Header.Set("Authorization", "Bearer "+access)
				continue
			}
		}
		return &Response{
			Response: res,
			Duration: req.Duration(),
		}, err
	}
}

func (c *Conn) doAndUnmarshal(req *Request, v any, templates ...any) error {
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	stream, err := bsupio.NewReader(req.Context(), super.NewContext(), res.Body, nil, nil, 1)
	if err != nil {
		return err
	}
	reader := sbuf.PullerReader(sbuf.NewMaterializer(stream))
	rec, err := reader.Read()
	if err != nil || rec == nil {
		return err
	}
	m := super.NewUnmarshaler()
	m.Bind(templates...)
	return m.Unmarshal(*rec, v)
}

// parseError parses an error from an http.Response with an error status code. For now the content type of errors is assumed to be JSON.
func parseError(r *http.Response) error {
	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		return err
	}
	resErr := &ErrorResponse{Response: r}
	if r.Header.Get("Content-Type") == api.MediaTypeJSON {
		var apierr api.Error
		if err := json.Unmarshal(body, &apierr); err != nil {
			return err
		}
		resErr.Err = &apierr
	} else {
		resErr.Err = errors.New(string(body))
	}
	return resErr
}

func errIsStatus(err error, code int) bool {
	var errRes *ErrorResponse
	return errors.As(err, &errRes) && errRes.StatusCode == code
}

func (c *Conn) NewRequest(ctx context.Context, method, path string, body any) *Request {
	req := newRequest(ctx, c.hostURL, c.defaultHeader.Clone())
	req.Method = method
	req.Path = path
	req.Body = body
	return req
}

// Ping checks to see if the server and measure the time it takes to
// get back the response.
func (c *Conn) Ping(ctx context.Context) (time.Duration, error) {
	req := c.NewRequest(ctx, http.MethodGet, "/status", nil)
	res, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	res.Body.Close()
	return res.Duration, nil
}

// Version retrieves the version string from the service.
func (c *Conn) Version(ctx context.Context) (string, error) {
	req := c.NewRequest(ctx, http.MethodGet, "/version", nil)
	var res api.VersionResponse
	if err := c.doAndUnmarshal(req, &res); err != nil {
		return "", err
	}
	return res.Version, nil
}

func (c *Conn) TableStats(ctx context.Context, id uuid.UUID) (exec.TableStats, error) {
	req := c.NewRequest(ctx, http.MethodGet, path.Join("/table", id.String(), "stats"), nil)
	var stats exec.TableStats
	err := c.doAndUnmarshal(req, &stats)
	if errIsStatus(err, http.StatusNotFound) {
		err = ErrTableNotFound
	}
	return stats, err
}

func (c *Conn) TableID(ctx context.Context, tableName string) (uuid.UUID, error) {
	if id, err := dbid.ParseID(tableName); err == nil {
		if _, err := dbconn.LookupTableByID(ctx, c, id); err == nil {
			return id, nil
		}
	}
	config, err := dbconn.LookupTableByName(ctx, c, tableName)
	if err != nil {
		return uuid.Nil(), err
	}
	return config.ID, nil
}

func (c *Conn) CommitObject(ctx context.Context, tableID uuid.UUID, branchName string) (uuid.UUID, error) {
	path := urlPath("table", tableID.String(), "branch", branchName)
	req := c.NewRequest(ctx, http.MethodGet, path, nil)
	var commit api.CommitResponse
	err := c.doAndUnmarshal(req, &commit)
	if errIsStatus(err, http.StatusNotFound) {
		err = ErrBranchNotFound
	}
	return commit.Commit, err
}

func (c *Conn) CreateTable(ctx context.Context, name string, sortKeys order.SortKeys, objectCap, frameCap uint64, typ super.Type, sql bool) (uuid.UUID, error) {
	message := api.TablePostRequest{
		Name:      name,
		SortKeys:  sortKeys,
		ObjectCap: objectCap,
		FrameCap:  frameCap,
		SQL:       sql,
	}
	if typ != nil {
		message.Type = sup.FormatTypeValueWithTypes(typ)
	}
	req := c.NewRequest(ctx, http.MethodPost, "/table", message)
	var meta db.BranchMeta
	err := c.doAndUnmarshal(req, &meta)
	if errIsStatus(err, http.StatusConflict) {
		err = ErrTableExists
	}
	return meta.Table.ID, err
}

func (c *Conn) RenameTable(ctx context.Context, tableID uuid.UUID, name string) error {
	if name == "" {
		return errors.New("no table name provided")
	}
	put := api.TablePutRequest{Name: name}
	req := c.NewRequest(ctx, http.MethodPut, path.Join("/table", tableID.String()), put)
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

func (c *Conn) RemoveTable(ctx context.Context, id uuid.UUID) error {
	req := c.NewRequest(ctx, http.MethodDelete, path.Join("/table", id.String()), nil)
	res, err := c.Do(req)
	if err != nil {
		if errIsStatus(err, http.StatusNotFound) {
			return ErrTableNotFound
		}
		return err
	}
	res.Body.Close()
	return nil
}

func (c *Conn) CreateBranch(ctx context.Context, tableID uuid.UUID, name string, at uuid.UUID) error {
	message := api.BranchPostRequest{
		Name:   name,
		Commit: at.String(),
	}
	req := c.NewRequest(ctx, http.MethodPost, path.Join("/table", tableID.String()), message)
	var branch branches.Config
	err := c.doAndUnmarshal(req, &branch)
	if errIsStatus(err, http.StatusConflict) {
		err = ErrBranchExists
	}
	return err
}

func (c *Conn) MergeBranch(ctx context.Context, tableID uuid.UUID, childBranch, parentBranch string, message dbconn.CommitMessage) (uuid.UUID, error) {
	path := urlPath("table", tableID.String(), "branch", parentBranch, "merge", childBranch)
	req := c.NewRequest(ctx, http.MethodPost, path, nil)
	if err := encodeCommitMessage(req, message); err != nil {
		return uuid.UUID{}, err
	}
	var commit api.CommitResponse
	err := c.doAndUnmarshal(req, &commit)
	return commit.Commit, err
}

func (c *Conn) RemoveBranch(ctx context.Context, tableID uuid.UUID, branchName string) error {
	return errors.New("TBD remote.RemoveBranch")
}

func (c *Conn) Revert(ctx context.Context, tableID uuid.UUID, branchName string, commitID uuid.UUID, message dbconn.CommitMessage) (uuid.UUID, error) {
	path := urlPath("table", tableID.String(), "branch", branchName, "revert", commitID.String())
	req := c.NewRequest(ctx, http.MethodPost, path, nil)
	if err := encodeCommitMessage(req, message); err != nil {
		return uuid.UUID{}, err
	}
	var commit api.CommitResponse
	err := c.doAndUnmarshal(req, &commit)
	return commit.Commit, err
}

// Query assembles a query from src and filenames and runs it.
//
// As for Conn.Do, if the returned error is nil, the user is expected to
// call Close on the returned vio.ScanCloser.
func (c *Conn) Query(ctx context.Context, inputs []srcfiles.Input) (vio.ScanCloser, error) {
	files, err := srcfiles.Concat(inputs)
	if err != nil {
		return nil, err
	}
	body := api.QueryRequest{Query: string(files.Text)}
	req := c.NewRequest(ctx, http.MethodPost, "/query?ctrl=T", body)
	res, err := c.Do(req)
	if ae := (*api.Error)(nil); errors.As(err, &ae) && len(ae.CompilationErrors) > 0 {
		ae.CompilationErrors.Bind(files)
		return nil, ae.CompilationErrors
	}
	if err != nil {
		return nil, err
	}
	q, err := queryio.NewScanner(ctx, res.Body)
	if err != nil {
		return nil, err
	}
	return &struct {
		vio.Scanner
		io.Closer
	}{
		Scanner: q,
		Closer:  res.Body,
	}, nil
}

func (c *Conn) Compact(ctx context.Context, tableID uuid.UUID, branchName string, objects []uuid.UUID, message dbconn.CommitMessage) (uuid.UUID, error) {
	path := urlPath("table", tableID.String(), "branch", branchName, "compact")
	req := c.NewRequest(ctx, http.MethodPost, path, api.CompactRequest{ObjectIDs: objects})
	if err := encodeCommitMessage(req, message); err != nil {
		return uuid.UUID{}, err
	}
	var commit api.CommitResponse
	err := c.doAndUnmarshal(req, &commit)
	return commit.Commit, err
}

// Load loads data from r.  contentType is a media type for r or the empty
// string, in which case the server will attempt to detect r's format.
func (c *Conn) Load(ctx context.Context, _ *super.Context, tableID uuid.UUID, branchName string, reader vio.Puller, message dbconn.CommitMessage) (uuid.UUID, error) {
	pr, pw := io.Pipe()
	go func() {
		w := bsupio.NewColumnWriter(sio.NopCloser(pw))
		err := vio.Copy(w, reader) //XXX need copywithcontext
		if err2 := w.Close(); err == nil {
			err = err2
		}
		pw.CloseWithError(err)
	}()
	// Closing the reader stops the writer goroutine if the request fails
	// before the body is consumed.
	defer pr.Close()
	path := urlPath("table", tableID.String(), "branch", branchName)
	req := c.NewRequest(ctx, http.MethodPost, path, pr)
	req.Header.Set("Content-Type", api.MediaTypeBSUP)
	if err := encodeCommitMessage(req, message); err != nil {
		return uuid.UUID{}, err
	}
	var commit api.CommitResponse
	err := c.doAndUnmarshal(req, &commit)
	if err != nil {
		return uuid.UUID{}, err
	}
	return commit.Commit, err
}

func encodeCommitMessage(req *Request, message dbconn.CommitMessage) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	req.Header.Set("SuperDB-Commit", string(encoded))
	return nil
}

func (c *Conn) Delete(ctx context.Context, tableID uuid.UUID, branchName string, ids []uuid.UUID, message dbconn.CommitMessage) (uuid.UUID, error) {
	return c.delete(ctx, tableID, branchName, ids, "", message)
}

func (c *Conn) DeleteWhere(ctx context.Context, tableID uuid.UUID, branchName, src string, message dbconn.CommitMessage) (uuid.UUID, error) {
	return c.delete(ctx, tableID, branchName, nil, src, message)
}

func (c *Conn) delete(ctx context.Context, tableID uuid.UUID, branchName string, ids []uuid.UUID, where string, message dbconn.CommitMessage) (uuid.UUID, error) {
	path := urlPath("table", tableID.String(), "branch", branchName, "delete")
	tags := make([]string, len(ids))
	for i, id := range ids {
		tags[i] = id.String()
	}
	req := c.NewRequest(ctx, http.MethodPost, path, api.DeleteRequest{
		ObjectIDs: tags,
		Where:     where,
	})
	if err := encodeCommitMessage(req, message); err != nil {
		return uuid.UUID{}, err
	}
	var commit api.CommitResponse
	err := c.doAndUnmarshal(req, &commit)
	return commit.Commit, err
}

func (c *Conn) Vacate(ctx context.Context, tableName string, ts nano.Ts, dryrun bool) ([]uuid.UUID, error) {
	path := urlPath("table", tableName, "vacate")
	vals := make(url.Values)
	vals.Add("ts", ts.Time().Format(time.RFC3339Nano))
	if dryrun {
		vals.Add("dryrun", "true")
	}
	path += "?" + vals.Encode()
	req := c.NewRequest(ctx, http.MethodPost, path, nil)
	var res api.VacateResponse
	err := c.doAndUnmarshal(req, &res)
	return res.CommitIDs, err
}

func (c *Conn) Vacuum(ctx context.Context, tableName, revision string, dryrun bool) ([]uuid.UUID, error) {
	path := urlPath("table", tableName, "revision", revision, "vacuum")
	if dryrun {
		path += "?dryrun=true"
	}
	req := c.NewRequest(ctx, http.MethodPost, path, nil)
	var res api.VacuumResponse
	err := c.doAndUnmarshal(req, &res)
	return res.ObjectIDs, err
}

func (c *Conn) SubscribeEvents(ctx context.Context) (*EventsClient, error) {
	req := c.NewRequest(ctx, http.MethodGet, "/events", nil)
	req.Header.Set("Accept", api.MediaTypeSUP)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	return newEventsClient(resp), nil
}

func (c *Conn) AuthMethod(ctx context.Context) (api.AuthMethodResponse, error) {
	req := c.NewRequest(ctx, http.MethodGet, "/auth/method", nil)
	var method api.AuthMethodResponse
	err := c.doAndUnmarshal(req, &method)
	return method, err
}

func (c *Conn) AuthIdentity(ctx context.Context) (api.AuthIdentityResponse, error) {
	req := c.NewRequest(ctx, http.MethodGet, "/auth/identity", nil)
	var ident api.AuthIdentityResponse
	err := c.doAndUnmarshal(req, &ident)
	return ident, err
}

func (c *Conn) refreshAuthToken(ctx context.Context) (string, error) {
	method, err := c.AuthMethod(ctx)
	if err != nil {
		return "", err
	}
	if method.Auth0 == nil {
		return "", fmt.Errorf("auth not available on database: %s", c.hostURL)
	}
	tokens, err := c.auth.Tokens(c.hostURL)
	if err != nil {
		return "", err
	}
	if tokens == nil {
		return "", fmt.Errorf("auth credentials not set for database: %s", c.hostURL)
	}
	client, err := auth0.NewClient(*method.Auth0)
	if err != nil {
		return "", err
	}
	refreshed, err := client.RefreshToken(ctx, tokens.Refresh)
	if err != nil {
		return "", err
	}
	if err := c.auth.SetTokens(c.hostURL, refreshed); err != nil {
		return "", err
	}
	c.SetAuthToken(refreshed.Access)
	return refreshed.Access, nil
}

type ErrorResponse struct {
	*http.Response
	Err error
}

func (e *ErrorResponse) Unwrap() error {
	return e.Err
}

func (e *ErrorResponse) Error() string {
	return fmt.Sprintf("status code %d: %v", e.StatusCode, e.Err)
}

func urlPath(elem ...string) string {
	var b strings.Builder
	for _, e := range elem {
		b.WriteString("/" + url.PathEscape(e))
	}
	return path.Clean(b.String())
}
