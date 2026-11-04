package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/anyio"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// instructions is the server's guidance to the model, which a client
// typically includes in the model's context.
const instructions = `This server queries a SuperDB database with SuperSQL, a superset of SQL
that also supports pipe syntax, e.g., "from logs | where status >= 500 | count() by path".
Use list_tables to find tables and describe_table to see a table's type.

Query results are JSON by default.  JSON can't represent all of SuperDB's types
(e.g., int64 vs. float64, timestamp, duration, ip, net, bytes, named types, unions, and
maps with non-string keys) so they are lost or approximated in JSON.  Request
format "sup" to get SUP, SuperDB's human-readable data format, which is a superset
of JSON that preserves every value's type exactly (e.g., 1, 1.5, 2025-01-01T00:00:00Z,
10.0.0.1, 1h30m, 80::uint16).  Prefer SUP when types matter.

See https://superdb.org/super-sql/intro.html for SuperSQL and
https://superdb.org/formats/sup.html for SUP.`

const readOnlyInstructions = `

This server is read-only: a query that modifies the database (e.g., INSERT INTO,
CREATE TABLE, DROP TABLE, or the load operator) is rejected.`

const loadInstructions = `

To add data to a table, use the load tool rather than an INSERT INTO query.  It
loads a file or inline data in any supported format (e.g., JSON, SUP, CSV, Parquet,
or Arrow) directly, which is much faster for more than a few values.`

func (s *Server) instructions() string {
	if s.opts.ReadOnly {
		return instructions + readOnlyInstructions
	}
	return instructions + loadInstructions
}

const defaultLimit = 1000

type tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations"`
}

func (s *Server) tools() []tool {
	queryDescription := `Run a SuperSQL query on the database and return its results, one value per line.
Format "json" (the default) returns JSON, which loses type information.  Format "sup"
returns SUP, which preserves SuperDB's types exactly (see the server instructions).
At most limit values are returned (default 1000).`
	queryAnnotations := map[string]any{"readOnlyHint": true, "openWorldHint": false}
	if s.opts.ReadOnly {
		queryDescription += "  The server is read-only, so a query that modifies the database is rejected."
	} else {
		queryDescription += "  A query may also modify the database (e.g., INSERT INTO or CREATE TABLE)."
		queryAnnotations = map[string]any{"readOnlyHint": false, "destructiveHint": true, "openWorldHint": false}
	}
	tools := []tool{
		{
			Name:        "query",
			Title:       "Run a SuperSQL query",
			Description: queryDescription,
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": `A SuperSQL query, e.g., "from logs | count() by status" or "SELECT count(*) FROM logs".`,
					},
					"format": map[string]any{
						"type":        "string",
						"enum":        []string{"json", "sup"},
						"default":     "json",
						"description": `Result format: "json" or "sup" (SUP preserves types).`,
					},
					"limit": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"default":     defaultLimit,
						"description": "Maximum number of values to return.",
					},
				},
				"required": []string{"query"},
			},
			Annotations: queryAnnotations,
		},
		{
			Name:        "list_tables",
			Title:       "List tables",
			Description: "List the database's tables with each table's name, ID, and, if it has one, its declared type in SUP type syntax.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			Annotations: map[string]any{"readOnlyHint": true, "openWorldHint": false},
		},
		{
			Name:  "describe_table",
			Title: "Describe a table",
			Description: `Return a table's type in SUP type syntax: its declared type if it has one and
otherwise the type of its data, which is computed from the type of each of its data
objects (without scanning its values).  By default, the type is superfused, i.e., a
fusion type shows exactly where the data's types vary (e.g., {a:int64,b:fusion(string|missing)}
means b is absent from some values).  Form "fused" returns the type without fusion
types (e.g., {a:int64,b:string|missing}).`,
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"table": map[string]any{"type": "string", "description": "The table's name."},
					"form": map[string]any{
						"type":        "string",
						"enum":        []string{"superfused", "fused"},
						"default":     "superfused",
						"description": `The form of an undeclared type: "superfused" or "fused".`,
					},
				},
				"required": []string{"table"},
			},
			Annotations: map[string]any{"readOnlyHint": true, "openWorldHint": false},
		},
	}
	if !s.opts.ReadOnly {
		tools = append(tools, loadToolDef)
	}
	return tools
}

var loadToolDef = tool{
	Name:  "load",
	Title: "Load data into a table",
	Description: `Load data into a table and commit it to a branch (default "main").  This is much
faster than an INSERT INTO query, especially for more than a few values.  Give exactly
one of: path, a file on the server's file system (or an S3 URI) in any supported format;
data, inline text data (e.g., JSON, SUP, or CSV); or data_base64, inline binary data
encoded as base64 (e.g., Parquet, Arrow IPC stream, or BSUP).  The format is detected
automatically unless format is given.  If the table has a type, each value is cast to it.
Returns the commit ID and the number of values loaded.`,
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"table":       map[string]any{"type": "string", "description": "The table's name or ID."},
			"branch":      map[string]any{"type": "string", "default": "main", "description": "The branch to commit to."},
			"path":        map[string]any{"type": "string", "description": "A file path (absolute is best) or S3 URI to load."},
			"data":        map[string]any{"type": "string", "description": "Inline text data to load."},
			"data_base64": map[string]any{"type": "string", "description": "Inline binary data to load, encoded as base64."},
			"format": map[string]any{
				"type":        "string",
				"default":     "auto",
				"description": `The data's format (e.g., "json", "sup", "csv", "tsv", "parquet", "arrows", "bsup", or "csup") or "auto" to detect it.`,
			},
			"message": map[string]any{"type": "string", "description": "The commit message."},
		},
		"required": []string{"table"},
	},
	Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false, "openWorldHint": false},
}

type toolFunc func(context.Context, json.RawMessage) (*toolResult, error)

func (s *Server) toolFunc(name string) (toolFunc, bool) {
	switch name {
	case "query":
		return s.queryTool, true
	case "list_tables":
		return s.listTablesTool, true
	case "describe_table":
		return s.describeTableTool, true
	case "load":
		return s.loadTool, true
	}
	return nil, false
}

type toolResult struct {
	Content []content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func textResult(texts ...string) *toolResult {
	r := &toolResult{}
	for _, t := range texts {
		r.Content = append(r.Content, content{Type: "text", Text: t})
	}
	return r
}

func errorResult(err error) *toolResult {
	r := textResult(err.Error())
	r.IsError = true
	return r
}

func unmarshalArgs(raw json.RawMessage, args any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, args); err != nil {
		return invalidArgs("%s", err)
	}
	return nil
}

func (s *Server) queryTool(ctx context.Context, raw json.RawMessage) (*toolResult, error) {
	var args struct {
		Query  string `json:"query"`
		Format string `json:"format"`
		Limit  *int   `json:"limit"`
	}
	if err := unmarshalArgs(raw, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Query) == "" {
		return nil, invalidArgs("query is required")
	}
	switch args.Format {
	case "":
		args.Format = "json"
	case "json", "sup":
	default:
		return nil, invalidArgs("format must be \"json\" or \"sup\"")
	}
	limit := defaultLimit
	if args.Limit != nil {
		if *args.Limit < 1 {
			return nil, invalidArgs("limit must be at least 1")
		}
		limit = *args.Limit
	}
	if s.opts.ReadOnly {
		if err := checkReadOnly(args.Query); err != nil {
			return nil, err
		}
	}
	text, truncated, err := runQuery(ctx, s.db, args.Query, args.Format, limit)
	if err != nil {
		return nil, err
	}
	switch {
	case truncated:
		return textResult(text, fmt.Sprintf("(results truncated to %d values; refine the query or raise limit)", limit)), nil
	case text == "":
		return textResult("(query produced no values)"), nil
	}
	return textResult(text), nil
}

// runQuery runs query on database and returns at most limit of its values
// in format and whether values were dropped to meet the limit.
func runQuery(ctx context.Context, database runtime.Database, query, format string, limit int) (string, bool, error) {
	q, err := database.Query(ctx, srcfiles.Plain(query))
	if err != nil {
		return "", false, err
	}
	defer q.Close()
	defer q.Pull(true)
	var buf bytes.Buffer
	w, err := anyio.NewWriter(sio.NopCloser(&buf), anyio.WriterOpts{Format: format})
	if err != nil {
		return "", false, err
	}
	l := &limiter{w: w, left: limit}
	err = vio.CopyMux(map[string]vio.Pusher{"main": l}, q)
	if closeErr := w.Close(); err == nil {
		err = closeErr
	}
	if err != nil && !errors.Is(err, errLimit) {
		return "", false, err
	}
	return strings.TrimSuffix(buf.String(), "\n"), l.truncated, nil
}

var errLimit = errors.New("limit reached")

// limiter pushes at most left values to w and then returns errLimit.
type limiter struct {
	w         vio.Pusher
	left      int
	truncated bool
}

func (l *limiter) Push(vec vector.Any) error {
	n := int(vec.Len())
	if n <= l.left {
		l.left -= n
		return l.w.Push(vec)
	}
	l.truncated = true
	if l.left > 0 {
		index := make([]uint32, l.left)
		for k := range index {
			index[k] = uint32(k)
		}
		if err := l.w.Push(vector.Pick(vec, index)); err != nil {
			return err
		}
		l.left = 0
	}
	return errLimit
}

type tableInfo struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	Type string `json:"type,omitempty"`
}

func (s *Server) listTablesTool(ctx context.Context, raw json.RawMessage) (*toolResult, error) {
	tables, err := s.root.ListTables(ctx)
	if err != nil {
		return nil, err
	}
	sctx := super.NewContext()
	infos := []tableInfo{}
	for _, t := range tables {
		info := tableInfo{Name: t.Name, ID: t.ID.String()}
		if len(t.Type) != 0 {
			typ, err := sctx.LookupByValue(t.Type)
			if err != nil {
				return nil, err
			}
			info.Type = sup.FormatType(typ)
		}
		infos = append(infos, info)
	}
	return jsonResult(infos)
}

func (s *Server) describeTableTool(ctx context.Context, raw json.RawMessage) (*toolResult, error) {
	var args struct {
		Table string `json:"table"`
		Form  string `json:"form"`
	}
	if err := unmarshalArgs(raw, &args); err != nil {
		return nil, err
	}
	if args.Table == "" {
		return nil, invalidArgs("table is required")
	}
	switch args.Form {
	case "":
		args.Form = "superfused"
	case "superfused", "fused":
	default:
		return nil, invalidArgs("form must be \"superfused\" or \"fused\"")
	}
	// Find the table among all tables (rather than with a query on its
	// name) so any name works.  (The tables come from the database's table
	// store rather than a query of :tables, whose results may share memory
	// with the query's buffers.)
	tables, err := s.root.ListTables(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range tables {
		if t.Name != args.Table {
			continue
		}
		desc := struct {
			Name     string `json:"name"`
			ID       string `json:"id"`
			Type     string `json:"type"`
			Declared bool   `json:"declared"`
		}{Name: t.Name, ID: t.ID.String()}
		sctx := super.NewContext()
		if len(t.Type) != 0 {
			typ, err := sctx.LookupByValue(t.Type)
			if err != nil {
				return nil, err
			}
			desc.Type, desc.Declared = sup.FormatType(typ), true
			return jsonResult(desc)
		}
		// The type of the table's data is the superfused type of its data
		// objects on the main branch.
		commit, err := s.db.CommitObject(ctx, t.ID, "main")
		if err != nil {
			return nil, err
		}
		typ, err := s.root.TableType(ctx, sctx, t.ID, commit)
		if err != nil {
			return nil, err
		}
		if typ == nil {
			return textResult(fmt.Sprintf("the type of table %q isn't known since it has no data", t.Name)), nil
		}
		if args.Form == "fused" {
			fuser := super.NewFuser(sctx)
			fuser.Fuse(typ)
			typ = fuser.SuperType()
		}
		desc.Type = sup.FormatType(typ)
		return jsonResult(desc)
	}
	return nil, fmt.Errorf("table %q not found", args.Table)
}

func (s *Server) loadTool(ctx context.Context, raw json.RawMessage) (*toolResult, error) {
	if s.opts.ReadOnly {
		return nil, errors.New("the server is read-only so loading is disabled")
	}
	var args struct {
		Table      string `json:"table"`
		Branch     string `json:"branch"`
		Path       string `json:"path"`
		Data       string `json:"data"`
		DataBase64 string `json:"data_base64"`
		Format     string `json:"format"`
		Message    string `json:"message"`
	}
	if err := unmarshalArgs(raw, &args); err != nil {
		return nil, err
	}
	if args.Table == "" {
		return nil, invalidArgs("table is required")
	}
	var n int
	for _, src := range []string{args.Path, args.Data, args.DataBase64} {
		if src != "" {
			n++
		}
	}
	if n != 1 {
		return nil, invalidArgs("exactly one of path, data, or data_base64 is required")
	}
	if args.Branch == "" {
		args.Branch = "main"
	}
	if args.Format == "" {
		args.Format = "auto"
	}
	tableID, err := s.db.TableID(ctx, args.Table)
	if err != nil {
		return nil, err
	}
	sctx := super.NewContext()
	opts := anyio.ReaderOpts{Format: args.Format}
	var r vio.Puller
	switch {
	case args.Path != "":
		f, err := anyio.Open(ctx, sctx, storage.NewLocalEngine(), args.Path, opts)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", args.Path, err)
		}
		defer f.Close()
		r = f
	case args.Data != "":
		if r, err = anyio.NewReader(ctx, sctx, strings.NewReader(args.Data), opts); err != nil {
			return nil, err
		}
	default:
		b, err := base64.StdEncoding.DecodeString(args.DataBase64)
		if err != nil {
			return nil, invalidArgs("data_base64: %s", err)
		}
		if r, err = anyio.NewReader(ctx, sctx, bytes.NewReader(b), opts); err != nil {
			return nil, err
		}
	}
	counter := &countingPuller{Puller: r}
	message := s.opts.Commit
	if author := authorFromContext(ctx); author != "" {
		message.Author = author
	}
	if args.Message != "" {
		message.Body = args.Message
	}
	commit, err := s.db.Load(ctx, sctx, tableID, args.Branch, counter, message)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		// A bare EOF from a reader means the data ended mid-value.
		return nil, fmt.Errorf("load: unexpected end of data after %d values (is the data truncated or not in format %q?)", counter.n, args.Format)
	}
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	return jsonResult(struct {
		Commit string `json:"commit"`
		Values int    `json:"values"`
	}{commit.String(), counter.n})
}

// countingPuller counts the values it pulls.
type countingPuller struct {
	vio.Puller
	n int
}

func (c *countingPuller) Pull(done bool) (vector.Any, error) {
	vec, err := c.Puller.Pull(done)
	if vec != nil {
		c.n += int(vec.Len())
	}
	return vec, err
}

func jsonResult(v any) (*toolResult, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	// SUP types like <int64> are more readable without HTML escaping.
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return textResult(strings.TrimSuffix(b.String(), "\n")), nil
}
