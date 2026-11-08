package mcp

import (
	"bytes"
	"cmp"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/db/tables"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/anyio"
	"github.com/superdb/super/sio/csvio"
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

` + fusionNotes + `

See https://superdb.org/super-sql/intro.html for SuperSQL and
https://superdb.org/formats/sup.html for SUP.`

// fusionDocs is where the docs describe fusion types.
const fusionDocs = "https://superdb.org/super-sql/operators/superfuse.html"

// fusionNotes explains how to read the fusion types in a superfused type.  It's
// part of the server's instructions and also accompanies any describe_table
// result whose type has a fusion type, so a client that drops the
// instructions still sees it.
const fusionNotes = `Reading types from describe_table: a superfused type summarizes heterogeneous
data in one type.  fusion(T1|T2|...) at a position means the values there vary
across these types in the data.  missing in a fusion means the value is absent:
a record field that some values lack, or, inside an array, no element (an empty
array).  For example, tags:fusion(string|missing|[fusion(string|missing)]) means
tags is sometimes a string, sometimes absent, and sometimes an array of strings
that may be empty.  When data is defused, an empty array with no other type
becomes [none].  Use form "fused" to get a plain union type instead.
See ` + fusionDocs + `.`

const readOnlyInstructions = `

This server is read-only: a query that modifies the database (e.g., INSERT INTO,
CREATE TABLE, DROP TABLE, or the load operator) is rejected.`

const loadInstructions = `

To add data to a table, use the load tool rather than an INSERT INTO query.  It
loads a file or inline data in a text format (e.g., JSON, SUP, CSV, or TSV)
directly, which is much faster for more than a few values.  To create
or drop a table, use the create_table or drop_table tool.`

const includeInstructions = `

The query tool's include argument names SuperSQL source files on the server
(e.g., shared declarations of constants, functions, and operators) whose text
precedes the query, so the query may use what they declare.`

func (s *Server) instructions() string {
	text := instructions
	if s.opts.IncludeDir != "" {
		text += includeInstructions
		if files := s.includeFiles(); len(files) > 0 {
			text += "  The include files are " + strings.Join(files, ", ") + "."
		}
	}
	if s.opts.ReadOnly {
		return text + readOnlyInstructions
	}
	return text + loadInstructions
}

const defaultLimit = 1000

// queryFormats are the output formats of the query tool.  Format "table" is
// CSV aligned in columns for reading (see renderTable).
var queryFormats = []string{"json", "sup", "csv", "tsv", "line", "table"}

// loadFormats are the input formats of the load tool.  Only text formats are
// offered since tool arguments are JSON text.
var loadFormats = []string{"auto", "json", "sup", "csv", "tsv", "line", "zeek"}

// quotedList formats list as, e.g., "a", "b", or "c".
func quotedList(list []string) string {
	q := make([]string, len(list))
	for k, s := range list {
		q[k] = strconv.Quote(s)
	}
	if len(q) < 3 {
		return strings.Join(q, " or ")
	}
	return strings.Join(q[:len(q)-1], ", ") + ", or " + q[len(q)-1]
}

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
Format "csv" or "tsv" returns a header line and a line per record, and format "table"
returns the same in columns aligned for reading; these require the results to be
records with the same fields (nested records are flattened to dotted names), so for
records with differing fields, append "| fuse" to the query.  Format "line" returns
each string as is (e.g., a log line) and any other value in SUP.
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
						"enum":        queryFormats,
						"default":     "json",
						"description": "Output format: " + quotedList(queryFormats) + `.  SUP preserves types.`,
					},
					"limit": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"default":     defaultLimit,
						"description": "Maximum number of values to return.",
					},
					"structured": map[string]any{
						"type":    "boolean",
						"default": false,
						"description": `With format "json", return the results as MCP structured content, i.e., an object
{"values":[...],"count":N,"truncated":false} whose values are the results, which is
also the result's text.  This is handy for a client that processes the results as
data rather than text, but the results are held in memory and sent twice, so use a
limit that keeps them small.`,
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
means b is absent from some values, and [fusion(string|missing)] means an array of
strings that is sometimes empty).  If the type has a fusion type, the result's notes
explain how to read it.  Form "fused" returns the type without fusion types (e.g.,
{a:int64,b:string|missing}).  See ` + fusionDocs + `.`,
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
	if s.opts.IncludeDir != "" {
		tools[0].InputSchema["properties"].(map[string]any)["include"] = s.includeSchema()
	}
	tools = append(tools, healthToolDef)
	if !s.opts.ReadOnly {
		tools = append(tools, loadToolDef, createTableToolDef, dropTableToolDef)
	}
	return tools
}

var loadToolDef = tool{
	Name:  "load",
	Title: "Load data into a table",
	Description: `Load data into a table and commit it to a branch (default "main").  This is much
faster than an INSERT INTO query, especially for more than a few values.  Give exactly
one of: path, a file on the server's file system (or an S3 URI), or data, inline text
data.  The format is detected automatically unless format is given.  If the table has a type, each value is cast to it.
Returns the commit ID and the number of values loaded.`,
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"table":  map[string]any{"type": "string", "description": "The table's name or ID."},
			"branch": map[string]any{"type": "string", "default": "main", "description": "The branch to commit to."},
			"path":   map[string]any{"type": "string", "description": "A file path (absolute is best) or S3 URI to load."},
			"data":   map[string]any{"type": "string", "description": "Inline text data to load."},
			"format": map[string]any{
				"type":        "string",
				"enum":        loadFormats,
				"default":     "auto",
				"description": "Input format: " + quotedList(loadFormats) + `.  "auto" detects it.  Format "line" loads each line as a string.`,
			},
			"message": map[string]any{"type": "string", "description": "The commit message."},
		},
		"required": []string{"table"},
	},
	Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false, "openWorldHint": false},
}

var healthToolDef = tool{
	Name:  "health",
	Title: "Check the server's health",
	Description: `Check that the server is up and its database is readable.  Returns status "ok",
the SuperDB version, whether the server is read-only, and the number of tables, or,
if the database can't be read, status "error" and the error.`,
	InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	Annotations: map[string]any{"readOnlyHint": true, "idempotentHint": true, "openWorldHint": false},
}

var createTableToolDef = tool{
	Name:  "create_table",
	Title: "Create a table",
	Description: `Create a table.  By default, a table has no type and accepts values of any type.
If type is given, each value loaded into the table is cast to it (e.g.,
{ts:timestamp,status:int64,path:string}).  If orderby is given, the table's data is
organized by that key (e.g., "ts" or "ts:desc").  Neither can be changed later.
Returns the table's name and ID.`,
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"table":         map[string]any{"type": "string", "description": "The new table's name."},
			"type":          map[string]any{"type": "string", "description": "A SUP type to which all values loaded into the table are cast."},
			"orderby":       map[string]any{"type": "string", "description": `A sort key with optional ":asc" or ":desc" suffix (e.g., "ts:desc").`},
			"if_not_exists": map[string]any{"type": "boolean", "default": false, "description": "If true, succeed without change when the table already exists."},
		},
		"required": []string{"table"},
	},
	Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false, "openWorldHint": false},
}

var dropTableToolDef = tool{
	Name:        "drop_table",
	Title:       "Drop a table",
	Description: "Drop a table, permanently deleting all of its data and branches.",
	InputSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"table":     map[string]any{"type": "string", "description": "The table's name or ID."},
			"if_exists": map[string]any{"type": "boolean", "default": false, "description": "If true, succeed without change when the table doesn't exist."},
		},
		"required": []string{"table"},
	},
	Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true, "idempotentHint": true, "openWorldHint": false},
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
	case "health":
		return s.healthTool, true
	case "create_table":
		return s.createTableTool, true
	case "drop_table":
		return s.dropTableTool, true
	}
	return nil, false
}

type toolResult struct {
	Content []content `json:"content"`
	// StructuredContent is the result as a JSON object (see the structured
	// argument of the query tool).
	StructuredContent any  `json:"structuredContent,omitempty"`
	IsError           bool `json:"isError,omitempty"`
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
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			if typeErr.Field == "" {
				return invalidArgs("arguments must be an object")
			}
			return invalidArgs("%s must be %s", typeErr.Field, jsonTypeName(typeErr.Type))
		}
		return invalidArgs("%s", err)
	}
	return nil
}

// jsonTypeName describes a Go type as the JSON type that unmarshals into it.
func jsonTypeName(typ reflect.Type) string {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Bool:
		return "a boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "an integer"
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.String:
		return "a string"
	case reflect.Slice, reflect.Array:
		return "an array"
	default:
		return "an object"
	}
}

func (s *Server) queryTool(ctx context.Context, raw json.RawMessage) (*toolResult, error) {
	var args struct {
		Query      string     `json:"query"`
		Format     string     `json:"format"`
		Limit      *int       `json:"limit"`
		Structured bool       `json:"structured"`
		Include    includeArg `json:"include"`
	}
	if err := unmarshalArgs(raw, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Query) == "" {
		return nil, invalidArgs("query is required")
	}
	inputs, err := s.includeInputs(args.Include)
	if err != nil {
		return nil, err
	}
	inputs = append(inputs, &srcfiles.PlainInput{Text: args.Query})
	if args.Format == "" {
		args.Format = "json"
	}
	if !slices.Contains(queryFormats, args.Format) {
		return nil, invalidArgs("format must be %s", quotedList(queryFormats))
	}
	if args.Structured && args.Format != "json" {
		return nil, invalidArgs("structured requires format \"json\"")
	}
	limit := defaultLimit
	if args.Limit != nil {
		if *args.Limit < 1 {
			return nil, invalidArgs("limit must be at least 1")
		}
		limit = *args.Limit
	}
	if s.opts.ReadOnly {
		// Check the included files too since they may modify the
		// database (e.g., with an operator declaration that loads).
		list, err := srcfiles.Concat(inputs)
		if err != nil {
			return nil, err
		}
		if err := checkReadOnly(list.Text); err != nil {
			return nil, err
		}
	}
	writeFormat := args.Format
	if writeFormat == "table" {
		writeFormat = "csv"
	}
	text, truncated, err := runQuery(ctx, s.db, inputs, writeFormat, limit)
	if errors.Is(err, csvio.ErrNotDataFrame) {
		return nil, fmt.Errorf("format %q requires records with the same fields but the results' fields differ (append \"| fuse\" to the query to unify records)", args.Format)
	}
	if err != nil {
		if strings.HasPrefix(err.Error(), "CSV output encountered non-record value") {
			return nil, fmt.Errorf("format %q requires records but a result is %s", args.Format, strings.TrimPrefix(err.Error(), "CSV output encountered non-record value: "))
		}
		return nil, err
	}
	if args.Format == "table" && text != "" {
		if text, err = renderTable(text); err != nil {
			return nil, err
		}
	}
	if args.Structured {
		return structuredResult(text, truncated)
	}
	switch {
	case truncated:
		return textResult(text, fmt.Sprintf("(results truncated to %s; refine the query or raise limit)", plural(limit, "value"))), nil
	case text == "":
		return textResult("(query produced no values)"), nil
	}
	return textResult(text), nil
}

// runQuery runs query on database and returns at most limit of its values
// in format and whether values were dropped to meet the limit.
func runQuery(ctx context.Context, database runtime.Database, query []srcfiles.Input, format string, limit int) (string, bool, error) {
	sctx := super.NewContext()
	q, err := database.Query(ctx, sctx, query)
	if err != nil {
		return "", false, err
	}
	defer q.Close()
	defer q.Pull(true)
	var buf bytes.Buffer
	w, err := anyio.NewWriter(sctx, sio.NopCloser(&buf), anyio.WriterOpts{Format: format})
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

// structuredResult returns a result whose structured content is an object
// holding the JSON values in text, one per line, and whose text is the same
// object, as MCP recommends for clients that don't support structured content.
func structuredResult(text string, truncated bool) (*toolResult, error) {
	result := struct {
		Values    []json.RawMessage `json:"values"`
		Count     int               `json:"count"`
		Truncated bool              `json:"truncated"`
	}{Values: []json.RawMessage{}, Truncated: truncated}
	dec := json.NewDecoder(strings.NewReader(text))
	for {
		var v json.RawMessage
		if err := dec.Decode(&v); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		result.Values = append(result.Values, v)
	}
	result.Count = len(result.Values)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(result); err != nil {
		return nil, err
	}
	b := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	r := textResult(string(b))
	r.StructuredContent = json.RawMessage(b)
	return r, nil
}

// renderTable renders CSV text as a table whose columns are aligned with
// spaces.  A newline or tab in a value is escaped so each record is a line.
func renderTable(text string) (string, error) {
	records, err := csv.NewReader(strings.NewReader(text)).ReadAll()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	escaper := strings.NewReplacer("\n", `\n`, "\r", `\r`, "\t", `\t`)
	for _, rec := range records {
		for k, field := range rec {
			if k > 0 {
				io.WriteString(w, "\t")
			}
			io.WriteString(w, escaper.Replace(field))
		}
		io.WriteString(w, "\n")
	}
	if err := w.Flush(); err != nil {
		return "", err
	}
	// Trailing spaces pad the last column, which only adds noise.
	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	for k, line := range lines {
		lines[k] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n"), nil
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
	// The table store's order varies so sort by name for stable results.
	slices.SortFunc(infos, func(a, b tableInfo) int { return cmp.Compare(a.Name, b.Name) })
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
			Notes    string `json:"notes,omitempty"`
		}{Name: t.Name, ID: t.ID.String()}
		sctx := super.NewContext()
		if len(t.Type) != 0 {
			typ, err := sctx.LookupByValue(t.Type)
			if err != nil {
				return nil, err
			}
			desc.Type, desc.Declared = sup.FormatType(typ), true
			if hasFusion(typ) {
				desc.Notes = fusionNotes
			}
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
		if hasFusion(typ) {
			desc.Notes = fusionNotes
		}
		return jsonResult(desc)
	}
	return nil, fmt.Errorf("table %q not found", args.Table)
}

// hasFusion reports whether typ is or contains a fusion type.
func hasFusion(typ super.Type) bool {
	switch typ := super.TypeUnder(typ).(type) {
	case *super.TypeFusion:
		return true
	case *super.TypeRecord:
		for _, f := range typ.Fields {
			if hasFusion(f.Type) {
				return true
			}
		}
	case *super.TypeArray:
		return hasFusion(typ.Type)
	case *super.TypeSet:
		return hasFusion(typ.Type)
	case *super.TypeMap:
		return hasFusion(typ.KeyType) || hasFusion(typ.ValType)
	case *super.TypeUnion:
		for _, t := range typ.Types {
			if hasFusion(t) {
				return true
			}
		}
	case *super.TypeError:
		return hasFusion(typ.Type)
	case *super.TypeOption:
		return hasFusion(typ.Type)
	}
	return false
}

func (s *Server) loadTool(ctx context.Context, raw json.RawMessage) (*toolResult, error) {
	if s.opts.ReadOnly {
		return nil, errors.New("the server is read-only so loading is disabled")
	}
	var args struct {
		Table   string `json:"table"`
		Branch  string `json:"branch"`
		Path    string `json:"path"`
		Data    string `json:"data"`
		Format  string `json:"format"`
		Message string `json:"message"`
	}
	if err := unmarshalArgs(raw, &args); err != nil {
		return nil, err
	}
	if args.Table == "" {
		return nil, invalidArgs("table is required")
	}
	if (args.Path == "") == (args.Data == "") {
		return nil, invalidArgs("exactly one of path or data is required")
	}
	if args.Branch == "" {
		args.Branch = "main"
	}
	if args.Format == "" {
		args.Format = "auto"
	}
	if !slices.Contains(loadFormats, args.Format) {
		return nil, invalidArgs("format must be %s", quotedList(loadFormats))
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
	default:
		if r, err = anyio.NewReader(ctx, sctx, strings.NewReader(args.Data), opts); err != nil {
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
		return nil, fmt.Errorf("load: unexpected end of data after %s (is the data truncated or not in format %q?)", plural(counter.n, "value"), args.Format)
	}
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	return jsonResult(struct {
		Commit string `json:"commit"`
		Values int    `json:"values"`
	}{commit.String(), counter.n})
}

func (s *Server) healthTool(ctx context.Context, raw json.RawMessage) (*toolResult, error) {
	type health struct {
		Status   string `json:"status"`
		Version  string `json:"version"`
		ReadOnly bool   `json:"read_only"`
		Tables   *int   `json:"tables,omitempty"`
		Error    string `json:"error,omitempty"`
	}
	h := health{Status: "ok", Version: s.version, ReadOnly: s.opts.ReadOnly}
	tables, err := s.root.ListTables(ctx)
	if err != nil {
		h.Status, h.Error = "error", err.Error()
		r, err := jsonResult(h)
		if err != nil {
			return nil, err
		}
		r.IsError = true
		return r, nil
	}
	n := len(tables)
	h.Tables = &n
	return jsonResult(h)
}

type tableResult struct {
	Table   string `json:"table"`
	ID      string `json:"id,omitempty"`
	Created bool   `json:"created,omitempty"`
	Dropped bool   `json:"dropped,omitempty"`
}

func (s *Server) createTableTool(ctx context.Context, raw json.RawMessage) (*toolResult, error) {
	if s.opts.ReadOnly {
		return nil, errors.New("the server is read-only so creating tables is disabled")
	}
	var args struct {
		Table       string `json:"table"`
		Type        string `json:"type"`
		OrderBy     string `json:"orderby"`
		IfNotExists bool   `json:"if_not_exists"`
	}
	if err := unmarshalArgs(raw, &args); err != nil {
		return nil, err
	}
	if args.Table == "" {
		return nil, invalidArgs("table is required")
	}
	sortKeys, err := order.ParseSortKeys(args.OrderBy)
	if err != nil {
		return nil, invalidArgs("orderby: %s", err)
	}
	var typ super.Type
	if args.Type != "" {
		if typ, err = sup.ParseType(super.NewContext(), args.Type); err != nil {
			return nil, invalidArgs("type %q: %s", args.Type, err)
		}
	}
	id, err := s.db.CreateTable(ctx, args.Table, sortKeys, 0, 0, typ, false)
	if errors.Is(err, tables.ErrExists) && args.IfNotExists {
		if id, err = s.root.TableID(ctx, args.Table); err == nil {
			return jsonResult(tableResult{Table: args.Table, ID: id.String()})
		}
	}
	if err != nil {
		return nil, err
	}
	return jsonResult(tableResult{Table: args.Table, ID: id.String(), Created: true})
}

func (s *Server) dropTableTool(ctx context.Context, raw json.RawMessage) (*toolResult, error) {
	if s.opts.ReadOnly {
		return nil, errors.New("the server is read-only so dropping tables is disabled")
	}
	var args struct {
		Table    string `json:"table"`
		IfExists bool   `json:"if_exists"`
	}
	if err := unmarshalArgs(raw, &args); err != nil {
		return nil, err
	}
	if args.Table == "" {
		return nil, invalidArgs("table is required")
	}
	id, err := s.db.TableID(ctx, args.Table)
	if errors.Is(err, tables.ErrNotFound) && args.IfExists {
		return jsonResult(tableResult{Table: args.Table})
	}
	if err != nil {
		return nil, err
	}
	if err := s.db.RemoveTable(ctx, id); err != nil {
		return nil, err
	}
	return jsonResult(tableResult{Table: args.Table, ID: id.String(), Dropped: true})
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

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
