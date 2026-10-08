package semantic

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/compiler/semantic/sem"
	"github.com/superdb/super/pkg/field"
)

// Analyze a SQL select expression which may have arbitrary nested subqueries
// and may or may not have its sources embedded.
// The output of a select expression is a record that wraps its input and
// selected columns in a record {in:any,out:any}.  The schema returned represents
// the observable scope of the selected elements.  When the parent operator is
// an OrderBy, it can reach into the "in" part of the select scope (for non-aggregates)
// and also sort by the out elements.  It's up to the caller to unwrap the in/out
// record when returning to pipeline context.
func (t *translator) sqlSelect(sel *ast.SQLSelect, demand []ast.Expr, seq sem.Seq, parentType super.Type) (sem.Seq, tableScope) {
	if len(sel.Selection.Args) == 0 {
		t.error(sel, errors.New("SELECT clause has no selection"))
		return seq, badTable
	}
	seq, fromScope := t.selectFrom(sel.Loc, sel.From, seq, parentType)
	if fromScope == nil || fromScope == badTable {
		return seq, badTable
	}
	if t.scope.sql != nil {
		fromScope = &subqueryScope{
			outer: t.scope.sql,
			inner: fromScope,
		}
	}
	save := t.scope.sql
	scope := &selectScope{
		in: fromScope,
	}
	t.scope.sql = scope
	defer func() {
		t.scope.sql = save
	}()
	// We first form column slots (and expand *'s) but delay analysis of expressions.
	// This will let us properly resolve lateral column aliases from any
	// GROUP BY expressions that reference them.
	fromType := fromScope.superType(t.sctx, t.checker.unknown)
	inType := t.sctx.MustLookupTypeRecord([]super.Field{super.NewField("in", fromType)})
	scope.columns = t.formProjection(scope, sel.Selection.Args, inType)
	var where sem.Expr
	if sel.Where != nil {
		var typ super.Type
		where, typ = t.expr(sel.Where, inType)
		t.checker.boolean(sel.Where, typ)
	}
	scope.lateral = true
	scope.groupings = t.groupBy(scope, sel.GroupBy, inType)
	scope.aggOk = true
	// Make sure any aggs needed by ORDER BY and HAVING are computed and
	// placed into the scope.aggs table.
	// This is a little tricky because we need to find the agg functions
	// and type check their args against the input table, but we can't
	// type check the outer parts of the expression yet because the output
	// scope is not available.  So we just call expr() here and ignore
	// any type errors.  This will work out ok because we descend into
	// expressions before type checking them so all agg funcs will be
	// encountered and entered into the scope.aggs table and the real
	// type checking will be handled above by the ORDER BY logic.
	t.checker.pushErrs()
	for _, e := range demand {
		t.expr(e, inType)
	}
	if sel.Having != nil {
		scope.lateral = false
		t.expr(sel.Having, inType)
		scope.lateral = true
	}
	t.checker.popErrs()

	// Next analyze all the columns so we know if there are aggregate functions
	// (in particular, we know this when there is no GROUP BY) so after this,
	// we know for sure if the output is grouped or not.
	scope.lateral = false
	t.projection(scope.columns, inType)
	scope.lateral = true
	if scope.isGrouped() {
		// Match grouping expressions in the projected column expressions.
		t.replaceGroupings(scope, scope.columns)
	} else if sel.Having != nil {
		t.error(sel.Having, errors.New("HAVING clause requires aggregation functions and/or a GROUP BY clause"))
	}
	// Now we stitch together the fragments into pipeline operators depending
	// on whether the ouput is grouped or not.
	seq = valuesExpr(wrapThis(sel, "in"), seq)
	if where != nil {
		seq = append(seq, sem.NewFilter(sel.Where, where))
	}
	if scope.isGrouped() {
		seq = t.genAggregate(sel.Loc, scope, seq)
		seq = valuesExpr(wrapThis(sel, "g"), seq)
	}
	seq, scope.out = t.emitProjection(scope.columns, scope.isGrouped(), seq)
	if sel.Having != nil {
		// We build the having expression after the projection but having
		// name resolution is relative to the before-projection scope while
		// we need the "out" type in the type-checker type in case "this"
		// is referenced in the having clause.  So we zero out scope.out
		// during the expr() traversal but leave it in place for superType().
		typ := scope.superType(t.sctx, t.checker.unknown)
		save := scope.out
		scope.out = nil
		scope.lateral = false
		having, typ := t.groupedExpr(scope, sel.Having, typ)
		scope.out = save
		scope.lateral = true
		if having != nil {
			t.checker.boolean(sel.Having, typ)
			seq = append(seq, sem.NewFilter(sel.Having, having))
		}
	}
	if sel.Distinct {
		seq = t.genDistinct(sem.NewThis(sel, field.NewChain("out")), seq)
	}
	return seq, scope
}

// groupedExpr translates e in the context of any grouping expressions where
// we translate any matching expression to its aggregate tmp variable and
// flag an error if we encounter un-matched references to the input table.
// When the select scope isn't an agg, this is like a normal t.expr().
func (t *translator) groupedExpr(scope *selectScope, e ast.Expr, inType super.Type) (sem.Expr, super.Type) {
	if e != nil {
		if !scope.isGrouped() {
			return t.expr(e, inType)
		}
		save := scope.grouped
		defer func() {
			scope.grouped = save
		}()
		scope.grouped = true
		expr, typ := t.expr(e, inType)
		if out, ok := replaceGroupings(t, expr, scope.groupings); ok {
			// Note that the replacements do not change the type here...
			return out, typ
		}
	}
	return nil, nil
}

func (t *translator) replaceGroupings(scope *selectScope, columns []column) {
	for k, c := range columns {
		columns[k].semExpr, _ = replaceGroupings(t, c.semExpr, scope.groupings)
	}
}

type column struct {
	name    string
	astExpr ast.Expr
	semExpr sem.Expr
	typ     super.Type
	lateral bool
}

func (t *translator) formProjection(scope *selectScope, in []ast.SQLAsExpr, inType super.Type) []column {
	var out []column
	scores := make(map[string]int)
	for _, as := range in {
		if star, ok := as.Expr.(*ast.StarExpr); ok {
			// expand * and table.* expressions
			paths, err := scope.star(star, star.Table, nil)
			if err != nil {
				t.error(as, err)
			}
			for _, p := range paths {
				typ := t.checker.this(star, p, inType)
				out = append(out, column{name: dedup(scores, t.asName(as.Label, nil, p.Chain.Path())), semExpr: p, typ: typ, astExpr: as.Expr})
			}
			continue
		}
		lateral := as.Label != nil && as.Label.Name != ""
		out = append(out, column{name: dedup(scores, t.asName(as.Label, as.Expr, nil)), astExpr: as.Expr, lateral: lateral})
	}
	return out
}

func (t *translator) asName(label *ast.ID, expr ast.Expr, path []string) string {
	var name string
	if label != nil {
		name = label.Name
		if name == "" {
			t.error(label, errors.New("column alias cannot be an empty string"))
		}
	} else if id, ok := expr.(*ast.IDExpr); ok && id.Name == "this" {
		name = "that"
	} else if path != nil {
		name = path[len(path)-1]
	} else {
		name = deriveNameFromExpr(expr)
	}
	return name
}

func (t *translator) projection(columns []column, inType super.Type) {
	for k := range columns {
		// Translates all expressions that weren't already expanded
		// from * patterns.
		if columns[k].semExpr == nil {
			e, typ := t.expr(columns[k].astExpr, inType)
			columns[k].semExpr = e
			columns[k].typ = typ
		}
	}
}

func (t *translator) emitProjection(columns []column, grouped bool, seq sem.Seq) (sem.Seq, *staticTable) {
	if len(columns) == 0 {
		return seq, nil
	}
	var outs []sem.RecordElem
	var fields []super.Field
	for _, c := range columns {
		outs = append(outs, &sem.FieldElem{
			Node:  c.astExpr,
			Name:  c.name,
			Value: c.semExpr,
		})
		fields = append(fields, super.NewField(c.name, c.typ))
	}
	var in string
	if grouped {
		in = "g"
	} else {
		in = "in"
	}
	loc := columns[0].astExpr //XXX
	e := &sem.RecordExpr{
		Node: loc,
		Elems: []sem.RecordElem{
			&sem.FieldElem{
				Node:  loc,
				Name:  in,
				Value: sem.NewThis(loc, field.NewChain(in)),
			},
			&sem.FieldElem{
				Node: loc,
				Name: "out",
				Value: &sem.RecordExpr{
					Node:  loc,
					Elems: outs,
				},
			},
		},
	}
	outType := t.sctx.MustLookupTypeRecord(fields)
	return append(seq, sem.NewValues(loc, e)), &staticTable{typ: outType}
}

func (t *translator) genAggregate(loc ast.Loc, scope *selectScope, seq sem.Seq) sem.Seq {
	var aggCols []sem.Assignment
	for k, agg := range scope.aggs {
		a := sem.Assignment{
			Node: agg.Node,
			LHS:  sem.NewThis(agg.Node, field.NewChain(aggTmp(k))),
			RHS:  agg,
		}
		aggCols = append(aggCols, a)
	}
	var keyCols []sem.Assignment
	for k, e := range scope.groupings {
		keyCols = append(keyCols, sem.Assignment{
			Node: e.loc,
			LHS:  sem.NewThis(loc, field.NewChain(groupTmp(k))),
			RHS:  e.expr,
		})
	}
	return append(seq, &sem.AggregateOp{
		Node: loc,
		Aggs: aggCols,
		Keys: keyCols,
	})
}

func aggTmp(k int) string {
	return fmt.Sprintf("t%d", k)
}

func groupTmp(k int) string {
	return fmt.Sprintf("k%d", k)
}

// selectFrom analyzes the FROM clause which is a cross-joined set of table expressions.
// inType is from the immediate pipe parent in a top-level select, nil otherwise
func (t *translator) selectFrom(loc ast.Loc, from ast.SQLTableExpr, seq sem.Seq, inType super.Type) (sem.Seq, relScope) {
	if from == nil {
		// No FROM clause means the pipe parent is providing input in which
		// case inType is the type of that input, or there is no input at all,
		// in which case we should model an empty table.
		if inType == nil || inType == super.TypeNull {
			// Model no or null input as an input table so that column checks fail.
			return seq, newTableFromType(t.sctx.MustLookupTypeRecord([]super.Field{}), t.checker.unknown)
		}
		return seq, newTableFromType(inType, t.checker.unknown)
	}
	off := len(seq)
	hasParent := off > 0
	seq, scope := t.sqlTableExpr(from, seq)
	if off >= len(seq) {
		// The chain didn't get lengthed so semFrom must have encountered
		// an error...
		return seq, badTable
	}
	// If we have parents with both a from and select, report an error but
	// only if it's not a RobotScan where the parent feeds the from operateor.
	if _, ok := seq[off].(*sem.RobotScan); !ok {
		if hasParent {
			t.error(loc, errors.New("SELECT cannot have both an embedded FROM clause and input from parents"))
			return append(seq, badOp), badTable
		}
	}
	return seq, scope
}

// sqlValues analyzes the values expression which currently cannot depend
// on the input data (until we add support for lateral joins; then a values
// clause in the FROM can refer to the left relation).
func (t *translator) sqlValues(values *ast.SQLValues, seq sem.Seq) (sem.Seq, *staticTable) {
	exprs := make([]sem.Expr, 0, len(values.Exprs))
	types := make([]super.Type, 0, len(values.Exprs))
	for _, astExpr := range values.Exprs {
		e, typ := t.expr(astExpr, super.TypeNull)
		exprs = append(exprs, e)
		types = append(types, typ)
	}
	seq = append(seq, sem.NewValues(values, exprs...))
	return seq, t.inferSchema(values, types)
}

// With type checking integrated into the SQL analysis, the logic here
// can always infer a staticTable.  Eventually, this will need to support
// lateral join subqueries which have runtime values (but staticTable columns are still known).
func (t *translator) inferSchema(loc ast.Node, types []super.Type) *staticTable {
	recType, ok := super.TypeUnder(t.checker.fuse(types)).(*super.TypeRecord)
	if !ok {
		t.error(loc, errors.New("VALUES clause must have records or tuples"))
		return badTable
	}
	return &staticTable{typ: recType}
}

func (t *translator) genDistinct(e sem.Expr, seq sem.Seq) sem.Seq {
	return append(seq, &sem.DistinctOp{
		Node: e,
		Expr: e,
	})
}

func (t *translator) sqlFromPipe(pipe *ast.SQLPipe, seq sem.Seq) (sem.Seq, relTable) {
	if query, ok := maybeSQLQueryBody(pipe); ok {
		seq, scope := t.sqlQueryBody(query, nil, seq, nil)
		return scope.endScope(query, seq)
	}
	if len(seq) > 0 {
		panic("semSQLOp: SQL pipes can't have parents")
	}
	// We pass in type null for initial type here since we know a pipe subquery inside
	// of a sql table expression must always have a data source (i.e., it cannot inherit
	// data from a parent node somewhere outside of this seq).  XXX support for lateral
	// join where the input to the pipe might depend on the table-expression to the left
	// may change how we wire things up here.
	seq, typ := t.seq(pipe.Body, super.TypeNull)
	return seq, newTableFromType(typ, t.checker.unknown)
}

func maybeSQLQueryBody(pipe *ast.SQLPipe) (ast.SQLQueryBody, bool) {
	if len(pipe.Body) == 1 {
		if op, ok := pipe.Body[0].(*ast.SQLOp); ok {
			return op.Body, true
		}
	}
	return nil, false
}

func applyAlias(sctx *super.Context, alias *ast.TableAlias, t relTable, seq sem.Seq) (sem.Seq, relTable, error) {
	if alias == nil || t == badTable {
		return seq, t, nil
	}
	if len(alias.Columns) == 0 {
		return seq, addTableAlias(t, alias.Name), nil
	}
	if t, ok := t.(*staticTable); ok {
		return mapColumns(sctx, t.typ, alias, seq)
	}
	return seq, t, errors.New("cannot apply column aliases to dynamically typed data")
}

func mapColumns(sctx *super.Context, in *super.TypeRecord, alias *ast.TableAlias, seq sem.Seq) (sem.Seq, *staticTable, error) {
	if len(alias.Columns) > len(in.Fields) {
		return nil, nil, fmt.Errorf("cannot apply %d column aliases in table alias %q to table with %d columns", len(alias.Columns), alias.Name, len(in.Fields))
	}
	typ := in
	out := idsToStrings(alias.Columns)
	if !slices.EqualFunc(in.Fields, out, func(f super.Field, name string) bool {
		return f.Name == name
	}) {
		// Make a record expression...
		elems := make([]sem.RecordElem, 0, len(in.Fields))
		fields := make([]super.Field, 0, len(in.Fields))
		for k := range out {
			elems = append(elems, &sem.FieldElem{
				Node:  alias.Columns[k],
				Name:  out[k],
				Value: sem.NewThis(alias.Columns[k], field.NewChainNullish(in.Fields[k].Name)),
			})
			fields = append(fields, super.NewField(out[k], in.Fields[k].Type))
		}
		seq = valuesExpr(&sem.RecordExpr{
			Node:  alias,
			Elems: elems,
		}, seq)
		typ = sctx.MustLookupTypeRecord(fields)
	}
	return seq, &staticTable{table: alias.Name, typ: typ}, nil
}

func idsToStrings(ids []*ast.ID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.Name)
	}
	return out
}

func (t *translator) sqlQueryBody(query ast.SQLQueryBody, demand []ast.Expr, seq sem.Seq, pipeType super.Type) (sem.Seq, tableScope) {
	switch query := query.(type) {
	case *ast.SQLSelect:
		return t.sqlSelect(query, demand, seq, pipeType)
	case *ast.SQLValues:
		return t.sqlValues(query, seq)
	case *ast.SQLQuery:
		if query.With != nil {
			old := t.sqlWith(query.With)
			defer func() { t.scope.ctes = old }()
		}
		var demand []ast.Expr
		if query.OrderBy != nil {
			demand = exprsFromSortExprs(query.OrderBy.Exprs)
		}
		seq, scope := t.sqlQueryBody(query.Body, demand, seq, pipeType)
		if scope == badTable {
			return seq, scope
		}
		if query.OrderBy != nil {
			seq = t.orderBy(query.OrderBy, scope, seq, scope.superType(t.sctx, t.checker.unknown))
		}
		if limoff := query.Limit; limoff != nil {
			if limoff.Offset != nil {
				seq = append(seq, &sem.SkipOp{Node: limoff.Offset, Count: t.mustEvalPositiveInteger(limoff.Offset)})
			}
			if limoff.Limit != nil {
				seq = append(seq, &sem.HeadOp{Node: limoff.Limit, Count: t.mustEvalPositiveInteger(limoff.Limit)})
			}
		}
		return seq, scope
	case *ast.SQLUnion:
		left, leftScope := t.sqlQueryBody(query.Left, nil, seq, nil)
		left, leftTable := leftScope.endScope(query.Left.(ast.Node), left)
		right, rightScope := t.sqlQueryBody(query.Right, nil, seq, nil)
		right, rightTable := rightScope.endScope(query.Right.(ast.Node), right)
		if leftTable == badTable || rightTable == badTable {
			return right, badTable
		}
		if leftTable.width() != rightTable.width() {
			t.error(query, errors.New("set operations can only be applied to sources with the same number of columns"))
			return sem.Seq{badOp}, badTable
		}
		if !slices.EqualFunc(leftTable.typ.Fields, rightTable.typ.Fields, func(f1 super.Field, f2 super.Field) bool {
			return f1.Name == f2.Name
		}) {
			// Rename fields on the right to match the left.
			var elems []sem.RecordElem
			for i, col := range leftTable.typ.Fields {
				elems = append(elems, &sem.FieldElem{
					Name: col.Name,
					Value: &sem.IndexExpr{
						Expr:  sem.NewThis(nil, nil),
						Index: sem.NewLiteral(nil, super.NewInt64(int64(i)), t.defs),
					},
				})
			}
			right = append(right, sem.NewValues(nil, &sem.RecordExpr{Elems: elems}))
		}
		out := sem.Seq{
			&sem.ForkOp{Node: query, Paths: []sem.Seq{left, right}},
			// This used to be dag.Combine but we don't have combine in the sem tree,
			// so we use a merge here.  If we don't put this in, then the optimizer
			// mysteriously removes the output/main from the end of the DAG.
			// The optimizer is too fussy/buggy in this way and we should clean it up.
			&sem.MergeOp{Node: query},
		}
		if query.Distinct {
			out = t.genDistinct(sem.NewThis(query, nil), out)
		}
		return out, leftTable
	default:
		panic(query)
	}
}

func exprsFromSortExprs(in []ast.SortExpr) []ast.Expr {
	var out []ast.Expr
	for _, e := range in {
		out = append(out, e.Expr)
	}
	return out
}

func (t *translator) orderBy(op *ast.SQLOrderBy, scope relScope, seq sem.Seq, inType super.Type) sem.Seq {
	save := t.scope.sql
	t.scope.sql = scope
	defer func() {
		t.scope.sql = save
	}()
	var exprs []sem.SortExpr
	for _, e := range op.Exprs {
		exprs = append(exprs, t.sortExpr(scope, e, false, inType))
	}
	return append(seq, &sem.SortOp{Node: op, Exprs: exprs})
}

func (t *translator) sqlWith(with *ast.SQLWith) map[string]*ast.SQLCTE {
	if with.Recursive {
		t.error(with, errors.New("recursive WITH queries not currently supported"))
	}
	old := t.scope.ctes
	t.scope.ctes = maps.Clone(t.scope.ctes)
	for k, c := range with.CTEs {
		// XXX Materialized option not currently supported.
		name := strings.ToLower(c.Name.Name)
		if _, ok := t.scope.ctes[name]; ok {
			t.error(c.Name, errors.New("duplicate WITH clause name"))
		}
		t.scope.ctes[name] = &with.CTEs[k]
	}
	return old
}

func (t *translator) sqlCrossJoin(join *ast.SQLCrossJoin, seq sem.Seq) (sem.Seq, relScope) {
	if len(seq) > 0 {
		// At some point we might want to let parent data flow into a join somehow,
		// but for now we flag an error.
		t.error(join, errors.New("SQL cross join cannot inherit data from pipeline parent"))
	}
	leftSeq, leftSchema := t.sqlTableExpr(join.Left, nil)
	return t.sqlAppendCrossJoin(join, leftSeq, leftSchema, join.Right)
}

func (t *translator) sqlAppendCrossJoin(node ast.Node, leftSeq sem.Seq, leftScope relScope, rhs ast.SQLTableExpr) (sem.Seq, relScope) {
	rightSeq, rightScope := t.sqlTableExpr(rhs, nil)
	sch := &joinScope{left: leftScope, right: rightScope}
	par := &sem.ForkOp{Paths: []sem.Seq{leftSeq, rightSeq}}
	dagJoin := &sem.JoinOp{
		Node:       node,
		Style:      "cross",
		LeftAlias:  "left",
		RightAlias: "right",
	}
	return sem.Seq{par, dagJoin}, sch
}

// For now, each joining table is on the right...
// We don't have logic to not care about the side of the JOIN ON keys...
func (t *translator) sqlJoin(join *ast.SQLJoin, seq sem.Seq) (sem.Seq, relScope) {
	if len(seq) > 0 {
		// At some point we might want to let parent data flow into a join somehow,
		// but for now we flag an error.
		t.error(join, errors.New("SQL join cannot inherit data from pipeline parent"))
	}
	leftSeq, leftScope := t.sqlTableExpr(join.Left, nil)
	rightSeq, rightScope := t.sqlTableExpr(join.Right, nil)
	var scope relScope
	if using, ok := join.Cond.(*ast.JoinUsingCond); ok {
		scope = newJoinUsingScope(&joinScope{left: leftScope, right: rightScope}, idsToStrings(using.Fields))
	} else {
		scope = &joinScope{left: leftScope, right: rightScope}
	}
	saved := t.scope.sql
	t.scope.sql = scope
	cond := t.sqlJoinCond(join.Cond, scope.superType(t.sctx, t.checker.unknown))
	t.scope.sql = saved
	style := join.Style
	if style == "" {
		style = "inner"
	}
	par := &sem.ForkOp{Paths: []sem.Seq{leftSeq, rightSeq}}
	dagJoin := &sem.JoinOp{
		Node:       join,
		Style:      style,
		LeftAlias:  "left",
		RightAlias: "right",
		Cond:       cond,
	}
	return sem.Seq{par, dagJoin}, scope
}

func (t *translator) sqlJoinCond(cond ast.JoinCond, typ super.Type) sem.Expr {
	switch cond := cond.(type) {
	case *ast.JoinOnCond:
		e, typ := t.expr(cond.Expr, typ)
		t.checker.boolean(cond.Expr, typ)
		return e
	case *ast.JoinUsingCond:
		jsch := t.scope.sql.(*joinUsingScope).joinScope
		var exprs []sem.Expr
		for _, id := range cond.Fields {
			left, _, err := jsch.left.resolveUnqualified(id.Name)
			if err != nil {
				t.error(id, err)
				continue
			}
			if left == nil {
				t.error(id, fmt.Errorf("column %q in USING clause does not exist in left table", id.Name))
				continue
			}
			right, _, err := jsch.right.resolveUnqualified(id.Name)
			if err != nil {
				t.error(id, err)
				continue
			}
			if right == nil {
				t.error(id, fmt.Errorf("column %q in USING clause does not exist in right table", id.Name))
				continue
			}
			lhs := sem.NewThis(id, field.NewChainNullish(append([]string{"left"}, left...)...))
			rhs := sem.NewThis(id, field.NewChainNullish(append([]string{"right"}, right...)...))
			exprs = append(exprs, sem.NewBinaryExpr(id, "==", lhs, rhs))
		}
		if len(exprs) == 0 {
			return badExpr
		}
		return andUsingExprs(cond, exprs)
	default:
		panic(cond)
	}
}

func andUsingExprs(cond ast.Node, exprs []sem.Expr) sem.Expr {
	n := len(exprs)
	e := exprs[n-1]
	for i := n - 2; i >= 0; i-- {
		e = sem.NewBinaryExpr(cond, "and", exprs[i], e)
	}
	return e
}

type exprloc struct {
	loc  ast.Node
	expr sem.Expr
	typ  super.Type
}

func (t *translator) groupBy(sch *selectScope, in []ast.Expr, inType super.Type) []exprloc {
	var out []exprloc
	for _, expr := range in {
		var typ super.Type
		var e sem.Expr
		if colno, ok := isOrdinal(expr); ok {
			if colno < 1 || colno > len(sch.columns) {
				t.error(expr, fmt.Errorf("position %d is not in select list", colno))
				typ = t.checker.unknown
			} else {
				save := sch.groupByLoc
				sch.groupByLoc = expr
				switch col := sch.columns[colno-1]; {
				case col.semExpr != nil:
					// If semExpr is not nil, column refers to an expanded star
					// expression - use this value.
					e, typ = col.semExpr, col.typ
				case col.lateral:
					e, typ = resolveLateralColumn(t, sch, col.name, inType)
				default:
					e, typ = t.expr(col.astExpr, inType)
				}
				sch.groupByLoc = save
			}
		} else {
			e, typ = t.expr(expr, inType)
		}
		out = append(out, exprloc{expr, e, typ})
	}
	return out
}

func isOrdinal(e ast.Expr) (int, bool) {
	if e, ok := e.(*ast.Primitive); ok && e.Type == "int64" {
		colno, err := strconv.Atoi(e.Text)
		if err != nil {
			panic(err)
		}
		return colno, true
	}
	return -1, false
}

func (t *translator) resolveOrdinalOuter(ts tableScope, n ast.Node, prefix string, col int) sem.Expr {
	switch ts := ts.(type) {
	case *selectScope:
		return t.resolveOrdinalOuter(ts.out, n, "out", col)
	case *staticTable:
		if col < 1 || col > ts.width() {
			t.error(n, fmt.Errorf("column %d is out of range", col))
			return badExpr
		}
		var path []string
		if prefix != "" {
			path = []string{prefix, ts.typ.Fields[col-1].Name}
		} else {
			path = []string{ts.typ.Fields[col-1].Name}
		}
		return sem.NewThis(n, field.NewChainNullish(path...))
	default:
		panic(ts)
	}
}

func dedup(scores map[string]int, s string) string {
	score, ok := scores[s]
	if !ok {
		scores[s] = 0
		return s
	}
	for {
		score++
		s2 := fmt.Sprintf("%s_%d", s, score)
		if _, ok := scores[s2]; !ok {
			scores[s] = score
			scores[s2] = 0
			return s2
		}
	}
}

func valuesExpr(e sem.Expr, seq sem.Seq) sem.Seq {
	return append(seq, sem.NewValues(e, e))
}

func wrapThis(n ast.Node, field string) *sem.RecordExpr {
	return wrapField(field, sem.NewThis(n, nil))
}

func wrapField(field string, e sem.Expr) *sem.RecordExpr {
	return &sem.RecordExpr{
		Node: e,
		Elems: []sem.RecordElem{
			&sem.FieldElem{
				Node:  e,
				Name:  field,
				Value: e,
			},
		},
	}
}
