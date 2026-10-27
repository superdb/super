package semantic

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/compiler/semantic/sem"
	"github.com/superdb/super/pkg/field"
)

// relTable is a dynamic input or static relational table
type relTable interface {
	relScope
	relTableType()
}

// tableScope is the entity used by the SQL query body logic
// where the termination of scope of a selectScope is delayed
// until ORDER BY can see it.
type tableScope interface {
	relScope
	endScope(n ast.Node, seq sem.Seq) (sem.Seq, *staticTable)
}

// relScope is a relational scope that can resolve names with respect
// to an arbitrarily nested set of relational entities.
type relScope interface {
	resolveQualified(table, col string) (field.Path, error)
	resolveUnqualified(col string) (field.Path, bool, error)
	resolveTable(n ast.Node, table string, path []string) (sem.Expr, bool, error)
	star(n ast.Node, table string, path field.Path) ([]*sem.ThisExpr, error)
	this(n ast.Node, path []string) sem.Expr
	superType(sctx *super.Context, unknown super.Type) super.Type
}

type (
	dynamicTable struct {
		table string
	}

	joinScope struct {
		left  relScope
		right relScope
	}

	// joinUsingScope specializes the joinScope so that * expansion
	// follows the ANSI standard by eliminating the duplicate using columns
	// from both sides and putting those columns in the leftmost positions.
	// The semantic logic checks that each column is present in both sides
	// of the join so there is no need to do that here and the unqualified
	// method matches the left side arbitrarily.
	joinUsingScope struct {
		*joinScope
		// The using condition columns
		columns []string
		skip    map[string]struct{}
	}

	// Schema of selectScope is...
	// {in:<input>,out:<projection>} for non-grouped
	// {g:{k0,k1,...,a0,a1,...},out:<projection>} for grouped
	selectScope struct {
		in relScope

		// aggOk set when it's ok to process agg functions in expressions
		aggOk bool

		// grouped is set when processing an expression that should resolve
		// "this" to the aggregate projection (i.e., "g" column)
		grouped bool

		// aggs is the set of agg functions computed in a grouped table
		// inclusive of agg functions used subsequently in an order by
		aggs     []*sem.AggFunc
		aggTypes []super.Type

		// groupings holds the grouping expressions which are needed to do
		// expression matching for having, order by, and the projection.
		// These expressions are always relative to the "in" scope.
		// The grouped field true when resolution has access only to the outputs.
		groupings []exprloc

		// groupByLoc is set when invoking t.expr() from an ordinal reference
		// so that we can produce a more understandable error message.
		groupByLoc ast.Node

		// Columns holds the projection.  We bookkeep both the ast expression
		// and sem tree expr so we can do lateral column alias macro substition.
		// The lateral field is true when lateral column resolution is allowed
		// during the analysis GROUP BY expressions.
		columns []column
		lateral bool

		// The output table is always static, e.g., a dynamic input is
		// always wrapped in a column.  Also, the projected columns referenced
		// in ORDER BY have precedence over the rest of the SELECT expressions
		// (in contrast to the rest of the SELECT body, where lateral column
		// aliases have lwoer precedecne).
		out *staticTable
	}

	staticTable struct {
		table string
		typ   *super.TypeRecord
	}

	subqueryScope struct {
		outer relScope
		inner relScope
	}
)

func newTableFromType(typ super.Type) relTable {
	if typ == badType {
		return badTable
	}
	if typ != nil {
		if recType := recordOf(typ); recType != nil {
			return &staticTable{typ: recType}
		}
	}
	return &dynamicTable{}
}

func recordOf(typ super.Type) *super.TypeRecord {
	switch typ := super.TypeUnder(typ).(type) {
	case *super.TypeRecord:
		return typ
	case *super.TypeUnion:
		if hasUnknown(typ) {
			return nil
		}
		// We assume here this is a fused type and thus has only one record
		// type in the union.
		for _, typ := range typ.Types {
			if typ := recordOf(typ); typ != nil {
				return typ
			}
		}
	case *super.TypeFusion:
		return recordOf(typ.Type)
	}
	return nil
}

func newJoinUsingScope(j *joinScope, columns []string) *joinUsingScope {
	skip := make(map[string]struct{})
	for _, c := range columns {
		skip[c] = struct{}{}
	}
	return &joinUsingScope{
		joinScope: j,
		columns:   columns,
		skip:      skip,
	}
}

func (*staticTable) relTableType() {}

func (s *staticTable) superType(sctx *super.Context, unknown super.Type) super.Type {
	if s.typ == nil {
		return unknown
	}
	return s.typ
}

func (s *staticTable) width() int {
	if s.typ == nil {
		return 0
	}
	return len(s.typ.Fields)
}

func (s *selectScope) isGrouped() bool {
	return len(s.aggs) != 0 || len(s.groupings) != 0
}

func (*dynamicTable) relTableType() {}

func (d *dynamicTable) superType(sctx *super.Context, unknown super.Type) super.Type {
	return unknown
}

func (d *dynamicTable) resolveQualified(table, col string) (field.Path, error) {
	if d.table == table {
		return field.Path{col}, nil
	}
	return nil, nil
}

func (j *joinScope) resolveQualified(table, col string) (field.Path, error) {
	left, err := j.left.resolveQualified(table, col)
	if err != nil {
		return nil, err
	}
	if left != nil {
		chk, err := j.right.resolveQualified(table, col)
		if err != nil {
			return nil, err
		}
		if chk != nil {
			return nil, fmt.Errorf("ambiguous qualified reference %q.%q", table, col)
		}
		return append([]string{"left"}, left...), nil
	}
	right, err := j.right.resolveQualified(table, col)
	if err != nil {
		return nil, err
	}
	if right != nil {
		return append([]string{"right"}, right...), nil
	}
	return nil, nil
}

func (s *selectScope) resolveQualified(table, col string) (field.Path, error) {
	path, err := s.in.resolveQualified(table, col)
	if path != nil {
		return append([]string{"in"}, path...), nil
	}
	return nil, err
}

func (s *staticTable) resolveQualified(table, col string) (field.Path, error) {
	if s == badTable {
		return nil, nil
	}
	if s.table == table && slices.ContainsFunc(s.typ.Fields, func(f super.Field) bool {
		return f.Name == col
	}) {
		return field.Path{col}, nil
	}
	return nil, nil
}

func (s *subqueryScope) resolveQualified(table, col string) (field.Path, error) {
	path, err := s.inner.resolveQualified(table, col)
	if err != nil {
		return nil, err
	}
	if path != nil {
		return path, nil
	}
	path, err = s.outer.resolveQualified(table, col)
	if err != nil {
		return nil, err
	}
	if path != nil {
		return nil, errors.New("correlated subqueries not currently supported")
	}
	return nil, nil
}

func (d *dynamicTable) resolveUnqualified(col string) (field.Path, bool, error) {
	return field.Path{col}, true, nil
}

func (j *joinScope) resolveUnqualified(col string) (field.Path, bool, error) {
	left, ldyn, err := j.left.resolveUnqualified(col)
	if ldyn && err == nil {
		err = fmt.Errorf("join on dynamic column %q requires table-qualified reference", col)
	}
	if err != nil {
		return nil, ldyn, err
	}
	if left != nil {
		chk, rdyn, err := j.right.resolveUnqualified(col)
		if rdyn && err == nil {
			err = fmt.Errorf("join on dynamic column %q requires table-qualified reference", col)
		}
		if err != nil {
			return nil, rdyn, err
		}
		if chk != nil {
			return nil, false, fmt.Errorf("ambiguous unqualified column %q", col)
		}
		return append([]string{"left"}, left...), false, nil
	}
	right, rdyn, err := j.right.resolveUnqualified(col)
	if rdyn && err == nil {
		err = fmt.Errorf("join on dynamic column %q requires table-qualified reference", col)
	}
	if err != nil {
		return nil, rdyn, err
	}
	if right != nil {
		return append([]string{"right"}, right...), rdyn, nil
	}
	return nil, false, nil
}

func (j *joinUsingScope) resolveUnqualified(col string) (field.Path, bool, error) {
	if slices.Contains(j.columns, col) {
		// avoid ambiguous column reference and return arbitrarily return left side
		left, dyn, err := j.left.resolveUnqualified(col)
		if err != nil {
			return nil, dyn, err
		}
		return append([]string{"left"}, left...), dyn, nil
	}
	// for non-using columns, ambiguous colomn references should be reported
	return j.joinScope.resolveUnqualified(col)
}

func (s *selectScope) resolveUnqualified(col string) (field.Path, bool, error) {
	// This just looks for column in the input table.  The resolve() function
	// looks at lateral column aliases if this fails.
	if s.out != nil && !s.isGrouped() {
		// The output scope is set after the select scope is almost closed so that
		// ORDER BY can utilize the projected columns, which has precedence
		// higher than the input table regardless of "pramga pg".
		// Otherwise, this query resolves the order-by to the input
		//   pragma pg
		//   select a as b from (values (0,3),(1,2),(2,0)) T(a,b)
		//   order by b
		// but it should use the output table b for order-by.
		// This isn't a problem when the table has aggregate output (!isGrouped())
		// (because there is no way to reference the input table values outside
		// of agg func arguments), and it can't work because expression matching
		// would be foiled by paths with out.col.
		path, dyn, err := s.out.resolveUnqualified(col)
		if err != nil {
			return nil, false, err
		}
		if path != nil {
			return append([]string{"out"}, path...), dyn, nil
		}
	}
	path, dyn, err := s.in.resolveUnqualified(col)
	if path != nil {
		return append([]string{"in"}, path...), dyn, nil
	}
	return nil, false, err
}

func (s *staticTable) resolveUnqualified(col string) (field.Path, bool, error) {
	if s == badTable {
		return nil, false, nil
	}
	if slices.ContainsFunc(s.typ.Fields, func(f super.Field) bool {
		return f.Name == col
	}) {
		return field.Path{col}, false, nil
	}
	return nil, false, nil
}

func (s *subqueryScope) resolveUnqualified(col string) (field.Path, bool, error) {
	path, dyn, err := s.inner.resolveUnqualified(col)
	if err != nil {
		return nil, dyn, err
	}
	if path != nil {
		return path, dyn, nil
	}
	path, dyn, err = s.outer.resolveUnqualified(col)
	if err != nil {
		return nil, dyn, err
	}
	if path != nil {
		return nil, dyn, errors.New("correlated subqueries not currently supported")
	}
	return nil, false, nil
}

func (d *dynamicTable) star(n ast.Node, table string, path field.Path) ([]*sem.ThisExpr, error) {
	return nil, errors.New("the all-columns (*) pattern cannot be used for dynamic inputs")
}

func (j *joinScope) star(n ast.Node, table string, path field.Path) ([]*sem.ThisExpr, error) {
	left, err := j.left.star(n, table, append(path, "left"))
	if err != nil {
		return nil, err
	}
	right, err := j.right.star(n, table, append(path, "right"))
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

func (j *joinUsingScope) star(n ast.Node, table string, path field.Path) ([]*sem.ThisExpr, error) {
	if table != "" {
		// If this is a table.* match, then we don't need to worry about putting
		// the USING columns on the left.  Just let the underlying join do the work.
		return j.joinScope.star(n, table, path)
	}
	// ANSI SQL says the USING columns are leftmost in the table, then the remaining.
	var out []*sem.ThisExpr
	for _, col := range j.columns {
		left, _, err := j.left.resolveUnqualified(col)
		if err != nil {
			return nil, err
		}
		p := append(append(path, "left"), left...)
		this := sem.NewThis(n, field.NewChainNullish(p...))
		out = append(out, this)
	}
	var err error
	out, err = j.filterAppend(out, j.left, n, append(path, "left"))
	if err != nil {
		return nil, err
	}
	return j.filterAppend(out, j.right, n, append(path, "right"))
}

func (j *joinUsingScope) filterAppend(out []*sem.ThisExpr, rs relScope, n ast.Node, path []string) ([]*sem.ThisExpr, error) {
	exprs, err := rs.star(n, "", path)
	if err != nil {
		return nil, err
	}
	for _, this := range exprs {
		if _, skip := j.skip[this.Chain[len(this.Chain)-1].ID]; !skip {
			out = append(out, this)
		}
	}
	return out, nil
}

func (s *selectScope) star(n ast.Node, table string, path field.Path) ([]*sem.ThisExpr, error) {
	return s.in.star(n, table, append(path, "in"))
}

func (s *staticTable) star(n ast.Node, table string, path field.Path) ([]*sem.ThisExpr, error) {
	if s == badTable {
		return nil, nil
	}
	var out []*sem.ThisExpr
	if table == "" || s.table == table {
		for _, col := range s.typ.Fields {
			path := field.NewChainNullish(path...).AppendNullish(col.Name)
			out = append(out, sem.NewThis(n, path))
		}
	}
	return out, nil
}

func (s *subqueryScope) star(n ast.Node, table string, path field.Path) ([]*sem.ThisExpr, error) {
	return s.inner.star(n, table, path)
}

// spread left/right join legs into "this"
func joinSpread(n ast.Node, left, right sem.Expr) *sem.RecordExpr {
	if left == nil {
		left = sem.NewThis(n, nil)
	}
	if right == nil {
		right = sem.NewThis(n, nil)
	}
	return &sem.RecordExpr{
		Node: n,
		Elems: []sem.RecordElem{
			&sem.SpreadElem{
				Node: n,
				Expr: left,
			},
			&sem.SpreadElem{
				Node: n,
				Expr: right,
			},
		},
	}
}

func (s *selectScope) endScope(n ast.Node, seq sem.Seq) (sem.Seq, *staticTable) {
	if s.out == nil {
		return seq, badTable
	}
	return valuesExpr(sem.NewThis(n, field.NewChain("out")), seq), s.out
}

func (s *staticTable) endScope(n ast.Node, seq sem.Seq) (sem.Seq, *staticTable) {
	return seq, s
}

func (d *dynamicTable) this(n ast.Node, path []string) sem.Expr {
	return sem.NewThis(n, field.NewChain(path...))
}

func (j *joinScope) this(n ast.Node, path []string) sem.Expr {
	left := j.left.this(n, append(path, "left"))
	right := j.right.this(n, append(path, "right"))
	return joinSpread(n, left, right)
}

func (s *selectScope) this(n ast.Node, path []string) sem.Expr {
	if s.grouped {
		return s.out.this(n, append(path, "out"))
	}
	return s.in.this(n, append(path, "in"))
}

func (s *staticTable) this(n ast.Node, path []string) sem.Expr {
	return sem.NewThis(n, field.NewChain(path...))
}

func (s *subqueryScope) this(n ast.Node, path []string) sem.Expr {
	return s.inner.this(n, append(path, "inner"))
}

func (d *dynamicTable) resolveTable(n ast.Node, table string, path []string) (sem.Expr, bool, error) {
	if d.table == table {
		// Can't refer to dynamic inputs by the table name because of the
		// table changes to static and has a column of that name, the query
		// semantics change.  Instead, consider "this".
		return nil, true, fmt.Errorf("illegal reference to table %q in dynamic input (consider using 'this')", table)
	}
	return nil, false, nil
}

func (j *joinScope) resolveTable(n ast.Node, name string, path []string) (sem.Expr, bool, error) {
	e, dyn, err := j.left.resolveTable(n, name, append(path, "left"))
	if err != nil {
		return nil, dyn, err
	}
	if e != nil {
		ambig, _, _ := j.right.resolveTable(n, name, append(path, "right"))
		if ambig != nil {
			return nil, dyn, fmt.Errorf("ambiguous table reference %q", name)
		}
		return e, dyn, nil
	}
	return j.right.resolveTable(n, name, append(path, "right"))
}

func (s *selectScope) resolveTable(n ast.Node, name string, path []string) (sem.Expr, bool, error) {
	return s.in.resolveTable(n, name, append(path, "in"))
}

func (s *staticTable) resolveTable(n ast.Node, table string, path []string) (sem.Expr, bool, error) {
	if s.table == table {
		return sem.NewThis(n, field.NewChain(path...)), false, nil
	}
	return nil, false, nil
}

func (s *subqueryScope) resolveTable(n ast.Node, table string, path []string) (sem.Expr, bool, error) {
	// XXX should search outer once we have suppport for correlated subq
	return s.inner.resolveTable(n, table, path)
}

func (d *dynamicTable) String() string {
	return "dynamic"
}

func (s *joinScope) String() string {
	return fmt.Sprintf("join:\n  left: %s\n  right: %s", s.left, s.right)
}

func (s *selectScope) String() string {
	return fmt.Sprintf("select:\n  in: %s\n  out: %s", s.in, s.out)
}

func (s *staticTable) String() string {
	return fmt.Sprintf("static %s", strings.Join(fieldsToNames(s.typ.Fields), ", "))
}

func fieldsToNames(columns []super.Field) []string {
	out := make([]string, 0, len(columns))
	for _, c := range columns {
		out = append(out, c.Name)
	}
	return out
}

func (s *subqueryScope) String() string {
	return fmt.Sprintf("subquery:\n  outer: %s\n  inner: %s", s.outer, s.inner)
}

func (j *joinScope) superType(sctx *super.Context, unknown super.Type) super.Type {
	return sctx.MustLookupTypeRecord([]super.Field{
		super.NewField("left", j.left.superType(sctx, unknown)),
		super.NewField("right", j.right.superType(sctx, unknown)),
	})
}

func (s *selectScope) superType(sctx *super.Context, unknown super.Type) super.Type {
	fields := []super.Field{super.NewField("in", s.in.superType(sctx, unknown))}
	if s.isGrouped() {
		fields = append(fields, super.NewField("g", s.gType(sctx)))
	}
	if s.out != nil {
		fields = append(fields, super.NewField("out", s.out.superType(sctx, unknown)))
	}
	return sctx.MustLookupTypeRecord(fields)
}

func (s *selectScope) gType(sctx *super.Context) super.Type {
	var fields []super.Field
	for k, e := range s.groupings {
		fields = append(fields, super.NewField(groupTmp(k), e.typ))
	}
	for k := range s.aggs {
		fields = append(fields, super.NewField(aggTmp(k), s.aggTypes[k]))
	}
	return sctx.MustLookupTypeRecord(fields)
}

func (s *subqueryScope) superType(sctx *super.Context, unknown super.Type) super.Type {
	// We are currently treating a subquery like the select body and
	// not wrapping inner/outer paths etc. so for proper type checking
	// we just use the inner type for now without any prefix.
	// When we add support for correlated subqueries, the logic will be
	// as commented out below.
	return s.inner.superType(sctx, unknown)
	//return sctx.MustLookupTypeRecord([]super.Field{
	//	super.NewField("inner", s.inner.superType(sctx, unknown)),
	//	super.NewField("outer", s.outer.superType(sctx, unknown)),
	//})
}

func addTableAlias(t relTable, table string) relTable {
	switch t := t.(type) {
	case *dynamicTable:
		return &dynamicTable{table: table}
	case *staticTable:
		return &staticTable{
			table: table,
			typ:   t.typ,
		}
	default:
		// There shouldn't be any other table types.
		panic(t)
	}
}
