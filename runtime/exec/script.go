package exec

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/db/commits"
	"github.com/superdb/super/db/tables"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// Script is a runtime.Query that runs the statements of an ast.Script one
// after the other, each to completion, stopping at the first statement
// that fails.  Each statement produces a status record after any values
// the statement itself produces (i.e., the results of a query statement).
// The status record of a failed statement includes its error, which the
// next Pull then returns.
type Script struct {
	rctx     *runtime.Context
	compiler runtime.Compiler
	env      *Environment
	ast      *parser.AST
	script   *ast.Script

	next  int
	qctx  *runtime.Context
	stmt  ast.Stmt
	start nano.Ts
	nvals uint64
	err   error
	done  bool
	// ended holds the output channels whose end-of-channel markers are
	// held back until the script finishes since later statements may
	// write to them.
	ended []string

	// mu protects query and progress, which Progress may access
	// concurrently with Pull.
	mu       sync.Mutex
	query    runtime.Query
	progress vio.Progress
}

var _ runtime.Query = (*Script)(nil)

// NewScript returns a runtime.Query for the script of p, using c to compile
// the queries of its statements.
func NewScript(rctx *runtime.Context, c runtime.Compiler, env *Environment, p *parser.AST) *Script {
	return &Script{
		rctx:     rctx,
		compiler: c,
		env:      env,
		ast:      p,
		script:   p.Script(),
	}
}

// Status is the status record produced by each statement of a script.
type Status struct {
	Kind  string  `super:"kind"`
	Stmt  string  `super:"stmt"`
	Start nano.Ts `super:"start"`
	End   nano.Ts `super:"end"`
	// Info is a record of Kind-specific information or nil if the
	// statement failed.
	Info any `super:"info"`
	// Error is the error string of a failed statement or nil.
	Error any `super:"error"`
}

type QueryInfo struct {
	Values uint64 `super:"values"`
}

type CreateTableInfo struct {
	Table   string    `super:"table"`
	ID      uuid.UUID `super:"id"`
	Created bool      `super:"created"`
}

type DropTableInfo struct {
	Table   string    `super:"table"`
	ID      uuid.UUID `super:"id"`
	Dropped bool      `super:"dropped"`
}

type InsertInfo struct {
	Table  string    `super:"table"`
	Values uint64    `super:"values"`
	Commit uuid.UUID `super:"commit"`
}

func (s *Script) Progress() vio.Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.progress
	if s.query != nil {
		p.Add(s.query.Progress())
	}
	return p
}

func (s *Script) addProgress(p vio.Progress) {
	s.mu.Lock()
	s.progress.Add(p)
	s.mu.Unlock()
}

func (s *Script) setQuery(q runtime.Query) {
	s.mu.Lock()
	s.query = q
	s.mu.Unlock()
}

func (s *Script) Pull(done bool) (vector.Any, error) {
	if done {
		s.finishQuery()
		s.done = true
		s.rctx.Cancel()
		return nil, nil
	}
	for {
		if s.err != nil {
			err := s.err
			s.err, s.done = nil, true
			return nil, err
		}
		if s.done {
			return nil, nil
		}
		if s.query != nil {
			vec, err := s.query.Pull(false)
			if err != nil {
				s.finishQuery()
				return s.status(nil, err), nil
			}
			if vec != nil {
				if labeled, ok := vec.(*vector.Labeled); ok {
					if labeled.Any == nil {
						s.endChannel(labeled.Label)
						continue
					}
					if labeled.Label == "main" {
						s.nvals += uint64(vec.Len())
					}
				} else if _, ok := vec.(*vector.Control); !ok {
					s.nvals += uint64(vec.Len())
				}
				return vec, nil
			}
			s.finishQuery()
			if err := s.rctx.Err(); err != nil {
				return s.status(nil, err), nil
			}
			return s.status(QueryInfo{Values: s.nvals}, nil), nil
		}
		if s.next >= len(s.script.Stmts) {
			if len(s.ended) > 0 {
				label := s.ended[0]
				s.ended = s.ended[1:]
				return &vector.Labeled{Label: label}, nil
			}
			s.done = true
			return nil, nil
		}
		if err := s.rctx.Err(); err != nil {
			s.done = true
			return nil, err
		}
		s.stmt = s.script.Stmts[s.next]
		s.next++
		s.start = nano.Now()
		s.nvals = 0
		switch stmt := s.stmt.(type) {
		case *ast.QueryStmt:
			s.qctx = runtime.NewContext(s.rctx.Context, s.rctx.Sctx)
			q, err := s.compile(s.qctx, stmt.Body)
			if err != nil {
				s.qctx.Cancel()
				return s.status(nil, err), nil
			}
			s.setQuery(q)
		default:
			info, err := s.command(stmt)
			return s.status(info, err), nil
		}
	}
}

func (s *Script) endChannel(label string) {
	if !slices.Contains(s.ended, label) {
		s.ended = append(s.ended, label)
	}
}

func (s *Script) finishQuery() {
	if q := s.query; q != nil {
		s.setQuery(nil)
		s.addProgress(q.Progress())
		q.Pull(true)
	}
	if s.qctx != nil {
		s.qctx.Cancel()
		s.qctx = nil
	}
}

func (s *Script) status(info any, err error) vector.Any {
	status := Status{
		Kind:  stmtKind(s.stmt),
		Stmt:  s.stmtText(s.stmt),
		Start: s.start,
		End:   nano.Now(),
	}
	if err != nil {
		status.Error = err.Error()
		s.err = err
	} else {
		status.Info = info
	}
	val, merr := super.NewMarshaler(s.rctx.Sctx).Marshal(status)
	if merr != nil {
		panic(merr)
	}
	s.endChannel("main")
	return &vector.Labeled{Any: sbuf.Dematerialize(s.rctx.Sctx, val), Label: "main"}
}

func stmtKind(stmt ast.Stmt) string {
	switch stmt.(type) {
	case *ast.QueryStmt:
		return "query"
	case *ast.CreateTableStmt:
		return "create_table"
	case *ast.DropTableStmt:
		return "drop_table"
	case *ast.InsertStmt:
		return "insert"
	default:
		return fmt.Sprintf("%T", stmt)
	}
}

func (s *Script) stmtText(stmt ast.Stmt) string {
	if files := s.ast.Files(); files != nil {
		if pos, end := stmt.Pos(), stmt.End(); pos >= 0 && end < len(files.Text) && pos <= end {
			return files.Text[pos : end+1]
		}
	}
	return ""
}

// compile compiles the query body of a statement with the script's
// declarations in scope.
func (s *Script) compile(rctx *runtime.Context, body ast.Seq) (runtime.Query, error) {
	seq := body
	if decls := s.script.Decls; len(decls) > 0 {
		seq = ast.Seq{&ast.ScopeOp{
			Kind:  "ScopeOp",
			Decls: decls,
			Body:  body,
			Loc:   ast.NewLoc(decls[0].Pos(), body.End()),
		}}
	}
	// The semantic pass doesn't modify the AST so the statements may share
	// the script's declarations.
	return s.compiler.NewQuery(rctx, s.ast.WithSeq(seq), 0)
}

func (s *Script) command(stmt ast.Stmt) (any, error) {
	if s.env == nil || s.env.DB() == nil {
		return nil, fmt.Errorf("%s requires a database", stmtName(stmt))
	}
	switch stmt := stmt.(type) {
	case *ast.CreateTableStmt:
		return s.createTable(stmt)
	case *ast.DropTableStmt:
		return s.dropTable(stmt)
	case *ast.InsertStmt:
		return s.insert(stmt)
	default:
		return nil, fmt.Errorf("unknown statement: %T", stmt)
	}
}

func stmtName(stmt ast.Stmt) string {
	switch stmt.(type) {
	case *ast.CreateTableStmt:
		return "CREATE TABLE"
	case *ast.DropTableStmt:
		return "DROP TABLE"
	case *ast.InsertStmt:
		return "INSERT"
	default:
		return fmt.Sprintf("%T", stmt)
	}
}

func (s *Script) createTable(stmt *ast.CreateTableStmt) (any, error) {
	root := s.env.DB()
	name := stmt.Name.Text
	info := CreateTableInfo{Table: name}
	if id, err := root.TableID(s.rctx, name); err == nil {
		if stmt.IfNotExists {
			info.ID = id
			return info, nil
		}
		return nil, fmt.Errorf("%s: %w", name, tables.ErrExists)
	} else if !errors.Is(err, tables.ErrNotFound) {
		return nil, err
	}
	typ, err := s.tableType(stmt)
	if err != nil {
		return nil, err
	}
	sortKeys, err := sortKeys(stmt.OrderBy)
	if err != nil {
		return nil, err
	}
	table, err := root.CreateTable(s.rctx, name, sortKeys, 0, 0, typ, len(stmt.Columns) > 0)
	if err != nil {
		if stmt.IfNotExists && errors.Is(err, tables.ErrExists) {
			// Another client created the table first.
			if id, err := root.TableID(s.rctx, name); err == nil {
				info.ID = id
				return info, nil
			}
		}
		return nil, err
	}
	info.ID = table.ID
	info.Created = true
	return info, nil
}

// tableType returns the type for the table created by stmt, which is the
// record type of its columns, where a column is nullable unless it's NOT
// NULL, or its type.
func (s *Script) tableType(stmt *ast.CreateTableStmt) (super.Type, error) {
	if stmt.Type != nil {
		return s.evalType(stmt.Type)
	}
	if len(stmt.Columns) == 0 {
		return nil, nil
	}
	sctx := s.rctx.Sctx
	var fields []super.Field
	for _, col := range stmt.Columns {
		typ, err := s.evalType(col.Type)
		if err != nil {
			return nil, err
		}
		if !col.NotNull {
			typ = sctx.Nullable(typ)
		}
		fields = append(fields, super.NewField(col.Name.Name, typ))
	}
	return sctx.LookupTypeRecord(fields)
}

// evalType returns the type for typ with the script's declarations in scope
// by compiling and running "values <typ>".
func (s *Script) evalType(typ ast.Type) (super.Type, error) {
	seq := ast.Seq{&ast.ValuesOp{
		Kind: "ValuesOp",
		Exprs: []ast.Expr{&ast.TypeValue{
			Kind:  "TypeValue",
			Value: typ,
			Loc:   ast.NewLoc(typ.Pos(), typ.End()),
		}},
		Loc: ast.NewLoc(typ.Pos(), typ.End()),
	}}
	rctx := runtime.NewContext(s.rctx.Context, s.rctx.Sctx)
	defer rctx.Cancel()
	q, err := s.compile(rctx, seq)
	if err != nil {
		return nil, err
	}
	defer q.Pull(true)
	vec, err := q.Pull(false)
	if err != nil {
		return nil, err
	}
	if vec == nil || vec.Len() != 1 {
		return nil, errors.New("internal error: type did not evaluate to a single value")
	}
	val := vector.ValueAt(scode.NewBuilder(), vec, 0)
	if val.Type() != super.TypeType {
		return nil, fmt.Errorf("not a type: %s", val)
	}
	return s.rctx.Sctx.LookupByValue(val.Bytes())
}

func sortKeys(orderBy *ast.SQLOrderBy) (order.SortKeys, error) {
	if orderBy == nil {
		return nil, nil
	}
	var keys order.SortKeys
	for _, e := range orderBy.Exprs {
		path, ok := exprToPath(e.Expr)
		if !ok {
			return nil, errors.New("ORDER BY of CREATE TABLE must be a field path")
		}
		if e.Nulls != nil {
			return nil, errors.New("NULLS FIRST and NULLS LAST are not supported in ORDER BY of CREATE TABLE")
		}
		which := order.Asc
		if e.Order != nil && e.Order.Name == "desc" {
			which = order.Desc
		}
		keys = append(keys, order.NewSortKey(which, path))
	}
	return keys, nil
}

func exprToPath(e ast.Expr) (field.Path, bool) {
	switch e := e.(type) {
	case *ast.IDExpr:
		return field.Path{e.Name}, true
	case *ast.BinaryExpr:
		if e.Op != "." {
			return nil, false
		}
		lhs, ok := exprToPath(e.LHS)
		if !ok {
			return nil, false
		}
		rhs, ok := e.RHS.(*ast.IDExpr)
		if !ok {
			return nil, false
		}
		return append(lhs, rhs.Name), true
	}
	return nil, false
}

func (s *Script) dropTable(stmt *ast.DropTableStmt) (any, error) {
	root := s.env.DB()
	name := stmt.Name.Text
	info := DropTableInfo{Table: name}
	id, err := root.TableID(s.rctx, name)
	if err != nil {
		if stmt.IfExists && errors.Is(err, tables.ErrNotFound) {
			return info, nil
		}
		return nil, err
	}
	if err := root.RemoveTable(s.rctx, id); err != nil {
		return nil, err
	}
	info.ID = id
	info.Dropped = true
	return info, nil
}

func (s *Script) insert(stmt *ast.InsertStmt) (any, error) {
	root := s.env.DB()
	name := stmt.Table.Text
	sctx := s.rctx.Sctx
	id, err := root.TableID(s.rctx, name)
	if err != nil {
		return nil, err
	}
	table, err := root.OpenTable(s.rctx, id)
	if err != nil {
		return nil, err
	}
	constraint, err := table.TypeConstraint(sctx)
	if err != nil {
		return nil, err
	}
	var shaper *insertShaper
	if cols := idNames(stmt.Columns); len(cols) > 0 || isSQL(stmt.Body) {
		if shaper, err = newInsertShaper(sctx, constraint, cols); err != nil {
			return nil, err
		}
	}
	qctx := runtime.NewContext(s.rctx.Context, sctx)
	defer qctx.Cancel()
	q, err := s.compile(qctx, stmt.Body)
	if err != nil {
		return nil, err
	}
	defer func() {
		s.addProgress(q.Progress())
		q.Pull(true)
	}()
	counter := &counter{parent: &unlabeler{q}}
	var puller vio.Puller = counter
	if shaper != nil {
		puller = shaper.puller(counter)
	}
	branch, err := table.OpenBranchByName(s.rctx, "main")
	if err != nil {
		return nil, err
	}
	commit, err := branch.Load(s.rctx, sctx, puller, "", "", "")
	if errors.Is(err, commits.ErrEmptyTransaction) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	return InsertInfo{Table: name, Values: counter.n, Commit: commit}, nil
}

func idNames(ids []*ast.ID) []string {
	var names []string
	for _, id := range ids {
		names = append(names, id.Name)
	}
	return names
}

// isSQL returns true if seq is a SQL query, whose result columns are
// assigned positionally to the columns of an INSERT.
func isSQL(seq ast.Seq) bool {
	if len(seq) != 1 {
		return false
	}
	switch op := seq[0].(type) {
	case *ast.SQLOp:
		return true
	case *ast.ScopeOp:
		return isSQL(op.Body)
	}
	return false
}

// unlabeler returns the values of its parent without channel labels and
// drops control vectors and end-of-channel markers.
type unlabeler struct {
	parent vio.Puller
}

func (u *unlabeler) Pull(done bool) (vector.Any, error) {
	for {
		vec, err := u.parent.Pull(done)
		if vec == nil || err != nil {
			return vec, err
		}
		if _, ok := vec.(*vector.Control); ok {
			continue
		}
		if vec, _ = vector.Unlabel(vec); vec != nil {
			return vec, nil
		}
	}
}

type counter struct {
	parent vio.Puller
	n      uint64
}

func (c *counter) Pull(done bool) (vector.Any, error) {
	vec, err := c.parent.Pull(done)
	if vec != nil {
		c.n += uint64(vec.Len())
	}
	return vec, err
}
