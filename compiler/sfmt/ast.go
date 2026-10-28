package sfmt

import (
	"fmt"
	"slices"
	"strings"

	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/runtime/expr/agg"
	"github.com/superdb/super/runtime/expr/function"
	"github.com/superdb/super/sup"
)

func AST(seq ast.Seq) string {
	if len(seq) == 0 {
		return ""
	}
	c := &canon{shared: shared{formatter{tab: 2}}, head: true, first: true}
	if len(seq) == 1 {
		if script, ok := seq[0].(*ast.Script); ok {
			c.script(script)
			c.flush()
			return c.String()
		}
	}
	c.query(seq)
	c.flush()
	return c.String()
}

func ASTExpr(e ast.Expr) string {
	d := &canon{
		shared: shared{formatter{tab: 2}},
		head:   true,
		first:  true,
	}
	d.expr(e, "")
	d.flush()
	return d.String()
}

type canon struct {
	shared
	head  bool
	first bool
}

func (c *canon) assignments(assignments []ast.Assignment) {
	for k, a := range assignments {
		if k > 0 {
			c.write(",")
		}
		c.assignment(a)
	}
}

func (c *canon) assignment(a ast.Assignment) {
	if a.LHS != nil {
		c.expr(a.LHS, "")
		c.write(":=")
	}
	c.expr(a.RHS, "")
}

func (c *canon) exprs(exprs []ast.Expr) {
	for k, e := range exprs {
		if k > 0 {
			c.write(", ")
		}
		c.expr(e, "")
	}
}

func (c *canon) expr(e ast.Expr, parent string) {
	switch e := e.(type) {
	case nil:
		c.write("null")
	case *ast.AggFuncExpr:
		var distinct string
		if e.Distinct {
			distinct = "distinct "
		}
		c.write("%s(%s", e.Name, distinct)
		if e.Expr != nil {
			c.expr(e.Expr, "")
		}
		c.write(")")
		if e.Filter != nil {
			c.write(" filter ")
			c.expr(e.Filter, "")
		}
	case *ast.SQLAsExpr:
		c.expr(e.Expr, "")
		if e.Label != nil {
			c.write(" as %s", e.Label.Name)
		}
	case *ast.Primitive:
		c.literal(*e)
	case *ast.IDExpr:
		c.write(e.Name)
	case *ast.DoubleQuoteExpr:
		c.write("%q", e.Text)
	case *ast.UnaryExpr:
		c.write(e.Op)
		c.expr(e.Operand, "not")
	case *ast.BinaryExpr:
		c.binary(e, parent)
	case *ast.CondExpr:
		c.write("(")
		c.expr(e.Cond, "")
		c.write(") ? ")
		c.expr(e.Then, "")
		c.write(" : ")
		c.expr(e.Else, "")
	case *ast.CallExpr:
		c.funcRefAsCall(e.Func)
		c.write("(")
		c.exprs(e.Args)
		c.write(")")
	case *ast.CaseExpr:
		c.write("case")
		if e.Expr != nil {
			c.write(" ")
			c.expr(e.Expr, "")
		}
		for _, when := range e.Whens {
			c.write(" when ")
			c.expr(when.Cond, "")
			c.write(" then ")
			c.expr(when.Then, "")
		}
		if e.Else != nil {
			c.write(" else ")
			c.expr(e.Else, "")
		}
		c.write(" end")
	case *ast.ExtractExpr:
		c.write("EXTRACT(")
		c.expr(e.Part, "")
		c.write(" FROM ")
		c.expr(e.Expr, "")
		c.write(")")
	case *ast.TypeValue:
		c.write("<")
		c.typ(e.Value)
		c.write(">")
	case *ast.RegexpExpr:
		c.write("/%s/", e.Pattern)
	case *ast.GlobExpr:
		c.write(e.Pattern)
	case *ast.IndexExpr:
		c.expr(e.Expr, "")
		c.write("[")
		c.expr(e.Index, "")
		c.write("]")
	case *ast.IsNullExpr:
		c.expr(e.Expr, "")
		c.write(" IS ")
		if e.Not {
			c.write("NOT ")
		}
		c.write("NULL")
	case *ast.SliceExpr:
		c.expr(e.Expr, "")
		c.write("[")
		if e.From != nil {
			c.expr(e.From, "")
		}
		c.write(":")
		if e.To != nil {
			c.expr(e.To, "")
		}
		c.write("]")
	case *ast.SearchTermExpr:
		c.write(e.Text)
	case *ast.StarExpr:
		if e.Table != "" {
			c.write(sup.QuotedName(e.Table))
			c.write(".")
		}
		c.write("*")
	case *ast.RecordExpr:
		c.write("{")
		for k, elem := range e.Elems {
			if k != 0 {
				c.write(",")
			}
			switch e := elem.(type) {
			case *ast.FieldElem:
				c.write(sup.QuotedName(e.Name.Text))
				if e.Opt {
					c.write("?")
				}
				c.write(":")
				c.expr(e.Value, "")
			case *ast.ExprElem:
				c.expr(e.Expr, "")
			case *ast.SpreadElem:
				c.write("...")
				c.expr(e.Expr, "")
			default:
				c.write("sfmt: unknown record elem type: %T", e)
			}
		}
		c.write("}")
	case *ast.CastExpr:
		c.write("CAST(")
		c.expr(e.Expr, "")
		c.write(" AS ")
		c.typ(e.Type)
		c.write(")")
	case *ast.SubstringExpr:
		c.write("SUBSTRING(")
		c.expr(e.Expr, "")
		if e.From != nil {
			c.write(" FROM ")
			c.expr(e.From, "")
		}
		if e.For != nil {
			c.write(" FOR ")
			c.expr(e.For, "")
		}
		c.write(")")
	case *ast.ArrayExpr:
		c.write("[")
		c.arrayElems(e.Elems)
		c.write("]")
	case *ast.SetExpr:
		c.write("set[")
		c.arrayElems(e.Elems)
		c.write("]")
	case *ast.MapExpr:
		c.write("map{")
		for k, e := range e.Entries {
			if k != 0 {
				c.write(",")
			}
			c.expr(e.Key, "")
			c.write(":")
			c.expr(e.Value, "")
		}
		c.write("}")
	case *ast.SubqueryExpr:
		open, close := "(", ")"
		if e.Array {
			open, close = "[", "]"
		}
		c.open(open)
		c.head = true
		c.seq(e.Body)
		c.close()
		c.ret()
		c.flush()
		c.write(close)
	case *ast.ExistsExpr:
		c.open("exists(")
		c.head = true
		c.seq(e.Body)
		c.close()
		c.ret()
		c.flush()
		c.write(")")
	case *ast.FStringExpr:
		c.write(`f"`)
		for _, elem := range e.Elems {
			switch elem := elem.(type) {
			case *ast.FStringExprElem:
				c.write("{")
				c.expr(elem.Expr, "")
				c.write("}")
			case *ast.FStringTextElem:
				c.write(elem.Text)
			default:
				c.write("(unknown f-string element %T)", elem)
			}
		}
		c.write(`"`)
	case *ast.BetweenExpr:
		c.write("(")
		c.expr(e.Expr, "")
		if e.Not {
			c.write(" not")
		}
		c.write(" between ")
		c.expr(e.Lower, "")
		c.write(" and ")
		c.expr(e.Upper, "")
		c.write(")")
	case *ast.SQLTimeExpr:
		c.write("%s %s", strings.ToUpper(e.Type), sup.QuotedString(e.Value.Text))
	case *ast.TupleExpr:
		c.write("(")
		c.exprs(e.Elems)
		c.write(")")
	default:
		c.write("(unknown expr %T)", e)
	}
}

func (c *canon) funcRefAsCall(f ast.Expr) {
	switch f := f.(type) {
	case *ast.FuncNameExpr:
		c.write("%s", f.Name)
	case *ast.LambdaExpr:
		c.lambda(f)
	default:
		c.expr(f, "")
	}
}

func (c *canon) lambda(lambda *ast.LambdaExpr) {
	c.write("(lambda ")
	c.ids(lambda.Params)
	c.write(":")
	c.expr(lambda.Expr, "")
	c.write(")")
}

func (c *canon) arrayElems(elems []ast.ArrayElem) {
	for k, elem := range elems {
		if k > 0 {
			c.write(",")
		}
		switch elem := elem.(type) {
		case *ast.SpreadElem:
			c.write("...")
			c.expr(elem.Expr, "")
		case *ast.ExprElem:
			c.expr(elem.Expr, "")
		}
	}
}

func (c *canon) binary(e *ast.BinaryExpr, parent string) {
	switch e.Op {
	case ".":
		if !isThis(e.LHS) {
			c.expr(e.LHS, "")
			c.write(".")
		}
		c.expr(e.RHS, "")
	case "and", "or", "in":
		parens := needsparens(parent, e.Op)
		c.maybewrite("(", parens)
		c.expr(e.LHS, e.Op)
		c.write(" %s ", e.Op)
		c.expr(e.RHS, e.Op)
		c.maybewrite(")", parens)
	default:
		parens := needsparens(parent, e.Op)
		c.maybewrite("(", parens)
		// do need parens calc
		c.expr(e.LHS, e.Op)
		c.write("%s", e.Op)
		c.expr(e.RHS, e.Op)
		c.maybewrite(")", parens)
	}
}

func needsparens(parent, op string) bool {
	return precedence(parent)-precedence(op) < 0
}

func precedence(op string) int {
	switch op {
	case "not":
		return 1
	case "^":
		return 2
	case "*", "/", "%":
		return 3
	case "+", "-":
		return 4
	case "<", "<=", ">", ">=", "==", "!=", "in":
		return 5
	case "and":
		return 6
	case "or":
		return 7
	default:
		return 100
	}
}

func isThis(e ast.Expr) bool {
	if id, ok := e.(*ast.IDExpr); ok {
		return id.Name == "this"
	}
	return false
}

func (c *canon) maybewrite(s string, do bool) {
	if do {
		c.write(s)
	}
}

func (c *canon) next() {
	if c.first {
		c.first = false
	} else {
		c.write("\n")
	}
	c.needRet = false
	c.writeTab()
	if c.head {
		c.head = false
	} else {
		c.write("| ")
	}
}

func (c *canon) decl(d ast.Decl) {
	switch d := d.(type) {
	case *ast.ConstDecl:
		c.write("const %s = ", d.Name.Name)
		c.expr(d.Expr, "")
	case *ast.FuncDecl:
		c.write("fn %s(", d.Name.Name)
		for i := range d.Lambda.Params {
			if i != 0 {
				c.write(", ")
			}
			c.write(d.Lambda.Params[i].Name)
		}
		c.open("): (")
		c.ret()
		c.expr(d.Lambda.Expr, d.Name.Name)
		c.close()
		c.ret()
		c.flush()
		c.write(")")
	case *ast.OpDecl:
		c.write("op %s ", d.Name.Name)
		for k, f := range d.Params {
			if k > 0 {
				c.write(", ")
			}
			c.write(f.Name)
		}
		c.open(": (")
		c.ret()
		c.flush()
		c.head = true
		c.seq(d.Body)
		c.close()
		c.ret()
		c.flush()
		c.write(")")
		c.head, c.first = true, true
	case *ast.TypeDecl:
		c.write("type %s = ", sup.QuotedName(d.Name.Name))
		c.typ(d.Type)
	case *ast.TypeQueryDecl:
		c.write("type %s ", sup.QuotedName(d.Name.Name))
		c.open("= (")
		c.ret()
		c.flush()
		c.head = true
		c.seq(d.Body)
		c.close()
		c.ret()
		c.flush()
		c.write(")")
		c.head, c.first = true, true
	default:
		c.open("unknown decl: %T", d)
		c.close()
	}

}

func (c *canon) seq(seq ast.Seq) {
	for _, p := range seq {
		c.op(p)
	}
}

func (c *canon) op(o ast.Op) {
	switch o := o.(type) {
	case *ast.ScopeOp:
		c.scope(o, true)
	case *ast.ForkOp:
		c.next()
		c.open("fork")
		for _, p := range o.Paths {
			c.ret()
			c.write("(")
			c.open()
			c.head = true
			c.seq(p)
			c.close()
			c.ret()
			c.write(")")
		}
		c.close()
		c.flush()
	case *ast.SwitchOp:
		c.next()
		c.write("switch")
		if o.Expr != nil {
			c.write(" ")
			c.expr(o.Expr, "")
		}
		for _, k := range o.Cases {
			c.ret()
			if k.Expr != nil {
				c.write("case ")
				c.expr(k.Expr, "")
			} else {
				c.write("default")
			}
			c.write(" (")
			c.open()
			c.head = true
			c.seq(k.Path)
			c.close()
			c.ret()
			c.write(")")
		}
		c.close()
		c.flush()
	case *ast.FromOp:
		c.next()
		c.write("from ")
		c.fromItem(o.Item)
	case *ast.AggregateOp:
		c.next()
		c.open("aggregate")
		c.ret()
		c.open()
		c.assignments(o.Aggs)
		if len(o.Keys) != 0 {
			c.write(" by ")
			c.assignments(o.Keys)
		}
		if o.Limit != 0 {
			c.write(" -with limit %d", o.Limit)
		}
		c.close()
		c.close()
	case *ast.CallOp:
		c.next()
		c.write("call %s ", sup.QuotedName(o.Name.Name))
		c.funcOrExprs(o.Args)
	case *ast.CountOp:
		c.next()
		c.write("count ")
		c.expr(o.Expr, "")
	case *ast.CutOp:
		c.next()
		c.write("cut ")
		c.assignments(o.Args)
	case *ast.DistinctOp:
		c.next()
		c.write("distinct ")
		c.expr(o.Expr, "")
	case *ast.DropOp:
		c.next()
		c.write("drop ")
		c.exprs(o.Args)
	case *ast.SortOp:
		c.next()
		c.write("sort")
		if o.Reverse {
			c.write(" -r")
		}
		c.sortExprs(o.Exprs)
	case *ast.LoadOp:
		c.next()
		c.write("load %s", sup.QuotedString(o.Table.Text))
		c.opArgs(o.Args)
	case *ast.HeadOp:
		c.next()
		c.open("head")
		if o.Count != nil {
			c.write(" ")
			c.expr(o.Count, "")
		}
		c.close()
	case *ast.TailOp:
		c.next()
		c.open("tail")
		if o.Count != nil {
			c.write(" ")
			c.expr(o.Count, "")
		}
		c.close()
	case *ast.UniqOp:
		c.next()
		c.write("uniq")
		if o.Cflag {
			c.write(" -c")
		}
	case *ast.PassOp:
		c.next()
		c.write("pass")
	case *ast.ExprOp:
		if agg := isAggFunc(o.Expr); agg != nil {
			c.op(agg)
			return
		}
		c.next()
		var which string
		e := o.Expr
		if IsSearch(e) {
			which = "search "
		} else if IsBool(e) {
			which = "where "
		} else if _, ok := e.(*ast.CallExpr); !ok {
			which = "values "
		}
		// Since we can't determine whether the expression is a func call or
		// an op call until the semantic pass, leave this ambiguous.
		// XXX (nibs) - I don't think we should be doing this kind introspection
		// here. This is why we have the semantic pass and the canonical format here
		// should reflect the ambiguous nature of the expression.
		if which != "" {
			c.open(which)
			defer c.close()
		}
		c.expr(e, "")
	case *ast.SearchOp:
		c.next()
		c.open("search ")
		c.expr(o.Expr, "")
		c.close()
	case *ast.WhereOp:
		c.next()
		c.open("where ")
		c.expr(o.Expr, "")
		c.close()
	case *ast.TopOp:
		c.next()
		c.write("top")
		if o.Reverse {
			c.write(" -r")
		}
		if o.Limit != nil {
			c.write(" ")
			c.expr(o.Limit, "")
		}
		c.sortExprs(o.Exprs)
	case *ast.PutOp:
		c.next()
		c.write("put ")
		c.assignments(o.Args)
	case *ast.RenameOp:
		c.next()
		c.write("rename ")
		c.assignments(o.Args)
	case *ast.FuseOp:
		c.next()
		if o.Complete {
			c.write("superfuse")
		} else {
			c.write("fuse")
		}
	case *ast.JoinOp:
		c.next()
		if o.Style != "" {
			c.write("%s ", o.Style)
		}
		c.write("join")
		if o.RightInput != nil {
			c.open(" (")
			c.head = true
			c.seq(o.RightInput)
			c.close()
			c.ret()
			c.flush()
			c.write(")")
		}
		if o.Alias != nil {
			c.write(" as {%s,%s}", o.Alias.Left.Name, o.Alias.Right.Name)
		}
		c.joinCond(o.Cond)
	case *ast.AssignmentOp:
		c.next()
		which := "put "
		if isAggAssignments(o.Assignments) {
			which = "aggregate "
		}
		c.open(which)
		c.assignments(o.Assignments)
		c.close()
	case *ast.MergeOp:
		c.next()
		c.write("merge")
		c.sortExprs(o.Exprs)
	case *ast.UnnestOp:
		c.unnest(o)
	case *ast.ValuesOp:
		c.next()
		c.write("values ")
		c.exprs(o.Exprs)
	case *ast.SQLOp:
		c.sqlQueryBody(o.Body)
	case *ast.OutputOp:
		c.next()
		c.write("output %s", o.Name.Name)
	case *ast.DebugOp:
		c.next()
		c.write("debug")
		if o.Expr != nil {
			c.write(" ")
			c.expr(o.Expr, "")
		}
		if o.Filter != nil {
			c.write(" filter (")
			c.expr(o.Filter, "")
			c.write(")")
		}
	default:
		panic(o)
	}
}

func (c *canon) sqlQueryBody(query ast.SQLQueryBody) {
	switch query := query.(type) {
	case *ast.SQLQuery:
		if with := query.With; with != nil {
			c.next()
			c.write("with ")
			if with.Recursive {
				c.write("recursive ")
			}
			for i, cte := range with.CTEs {
				if i > 0 {
					c.write(", ")
				}
				c.write("%s as ", cte.Name.Name)
				if cte.Materialized {
					c.write("materialized ")
				}
				c.open("(")
				c.sqlQueryBody(cte.Body)
				c.close()
				c.ret()
				c.write(")")
			}
		}
		c.head = true
		c.sqlQueryBody(query.Body)
		if query.OrderBy != nil {
			c.ret()
			c.write("order by")
			c.sortExprs(query.OrderBy.Exprs)
		}
		if limoff := query.Limit; limoff != nil {
			c.ret()
			c.write("limit ")
			c.expr(limoff.Limit, "")
			if limoff.Offset != nil {
				c.ret()
				c.write("offset ")
				c.expr(limoff.Offset, "")
			}
		}
	case *ast.SQLSelect:
		c.next()
		c.write("select ")
		if query.Distinct {
			c.write("distinct ")
		}
		for i, a := range query.Selection.Args {
			if i > 0 {
				c.write(", ")
			}
			c.expr(&a, "")
		}
		if query.From != nil {
			c.head = true
			c.ret()
			c.write("from ")
			c.tableExpr(query.From)
		}
		if query.Where != nil {
			c.ret()
			c.write("where ")
			c.expr(query.Where, "")
		}
		if len(query.GroupBy) > 0 {
			c.ret()
			c.write("group by ")
			c.exprs(query.GroupBy)
		}
		if query.Having != nil {
			c.ret()
			c.write("having ")
			c.expr(query.Having, "")
		}
	case *ast.SQLUnion:
		c.sqlQueryBody(query.Left)
		c.ret()
		c.write("union")
		if query.Distinct {
			c.write(" distinct")
		} else {
			c.write(" all")
		}
		c.head = true
		c.sqlQueryBody(query.Right)
	case *ast.SQLValues:
		c.next()
		c.write("values ")
		c.exprs(query.Exprs)
	default:
		panic(query)
	}
}

func (c *canon) funcOrExprs(args []ast.Expr) {
	for k, a := range args {
		if k > 0 {
			c.write(", ")
		}
		switch a := a.(type) {
		case *ast.LambdaExpr:
			c.lambda(a)
		case *ast.FuncNameExpr:
			c.write("&%s", a.Name)
		default:
			c.expr(a, "")
		}
	}
}

func (c *canon) ids(ids []*ast.ID) {
	for k, id := range ids {
		if k > 0 {
			c.write(", ")
		}
		c.write(id.Name)
	}
}

func (c *canon) fromItem(item *ast.FromItem) {
	c.fromSource(item.Source)
	c.opArgs(item.Args)
}

func (c *canon) sqlFromItem(item *ast.SQLFromItem) {
	switch input := item.Input.(type) {
	case *ast.FromItem:
		c.fromItem(input)
	case *ast.SQLPipe:
		c.sqlPipe(input)
	default:
		panic(input)
	}
	if item.Alias != nil {
		c.tableAlias(item.Alias)
	}
}

func (c *canon) tableAlias(alias *ast.TableAlias) {
	c.write(" as %s", sup.QuotedName(alias.Name))
	if len(alias.Columns) != 0 {
		c.write(" (")
		var comma string
		for _, col := range alias.Columns {
			c.write("%s%s", comma, sup.QuotedName(col.Name))
			comma = ", "
		}
		c.write(")")
	}
}

func (c *canon) fromSource(s ast.FromSource) {
	switch s := s.(type) {
	case *ast.FromEval:
		c.expr(s.Expr, "")
	case *ast.GlobExpr:
		c.write(s.Pattern)
	case *ast.RegexpExpr:
		c.write("/" + s.Pattern + "/")
	case *ast.Text:
		c.write(sup.QuotedName(s.Text))
	default:
		panic(s)
	}
}

func (c *canon) tableExpr(e ast.SQLTableExpr) {
	switch e := e.(type) {
	case *ast.SQLFromItem:
		c.sqlFromItem(e)
	case *ast.SQLCrossJoin:
		c.tableExpr(e.Left)
		c.ret()
		c.write("cross join ")
		c.tableExpr(e.Right)
	case *ast.SQLJoin:
		c.tableExpr(e.Left)
		c.ret()
		if e.Style != "" {
			c.write(e.Style + " ")
		}
		c.write("join ")
		c.tableExpr(e.Right)
		c.joinCond(e.Cond)
	default:
		panic(fmt.Sprintf("unknown from expression: %T", e))
	}
}

func (c *canon) sqlPipe(pipe *ast.SQLPipe) {
	c.open("(")
	c.head = true
	c.seq(pipe.Body)
	c.close()
	c.ret()
	c.write(")")
}

func (c *canon) joinCond(e ast.JoinCond) {
	switch e := e.(type) {
	case *ast.JoinOnCond:
		c.write(" on ")
		c.expr(e.Expr, "")
	case *ast.JoinUsingCond:
		c.write(" using (")
		c.ids(e.Fields)
		c.write(")")
	default:
		panic(e)
	}
}

func (c *canon) unnest(o *ast.UnnestOp) {
	c.next()
	c.write("unnest ")
	c.expr(o.Expr, "")
	if o.Body != nil {
		c.write(" into (")
		c.open()
		c.head = true
		c.seq(o.Body)
		c.close()
		c.ret()
		c.flush()
		c.write(")")
	}
}

func (c *canon) scope(s *ast.ScopeOp, parens bool) {
	if parens {
		c.open("(")
		c.ret()
	}
	for _, d := range s.Decls {
		c.decl(d)
		c.ret()
	}
	//XXX functions?
	c.flush()
	c.seq(s.Body)
	if parens {
		c.close()
		c.ret()
		c.flush()
		c.write(")")
	}
}

func (c *canon) query(seq ast.Seq) {
	if len(seq) > 0 {
		if scope, ok := seq[0].(*ast.ScopeOp); ok {
			c.scope(scope, false)
			return
		}
	}
	c.seq(seq)
}

func (c *canon) script(s *ast.Script) {
	for _, d := range s.Decls {
		c.decl(d)
		c.ret()
	}
	for k, stmt := range s.Stmts {
		if k > 0 {
			c.write(";")
			c.ret()
			c.flush()
		}
		c.head, c.first = true, true
		c.stmt(stmt)
	}
}

func (c *canon) stmt(stmt ast.Stmt) {
	switch stmt := stmt.(type) {
	case *ast.QueryStmt:
		c.query(stmt.Body)
	case *ast.CreateTableStmt:
		if stmt.Type != nil {
			c.write("create super table ")
		} else {
			c.write("create table ")
		}
		if stmt.IfNotExists {
			c.write("if not exists ")
		}
		c.write(sup.QuotedString(stmt.Name.Text))
		if len(stmt.Columns) > 0 {
			c.write(" (")
			for k, col := range stmt.Columns {
				if k > 0 {
					c.write(", ")
				}
				c.write("%s ", sup.QuotedName(col.Name.Name))
				c.typ(col.Type)
				if col.NotNull {
					c.write(" not null")
				}
			}
			c.write(")")
		} else if stmt.Type != nil {
			c.write(" ")
			c.typ(stmt.Type)
		}
		if stmt.OrderBy != nil {
			c.write(" order by")
			c.sortExprs(stmt.OrderBy.Exprs)
		}
	case *ast.DropTableStmt:
		c.write("drop table ")
		if stmt.IfExists {
			c.write("if exists ")
		}
		c.write(sup.QuotedString(stmt.Name.Text))
	case *ast.InsertStmt:
		c.write("insert into %s", sup.QuotedString(stmt.Table.Text))
		if len(stmt.Columns) > 0 {
			c.write(" (")
			c.ids(stmt.Columns)
			c.write(")")
		}
		c.ret()
		c.flush()
		c.head, c.first = true, true
		c.query(stmt.Body)
	default:
		c.write("unknown statement: %T", stmt)
	}
}

func (c *canon) sortExprs(sortExprs []ast.SortExpr) {
	for i, s := range sortExprs {
		if i > 0 {
			c.write(",")
		}
		c.space()
		c.expr(s.Expr, "")
		if s.Order != nil {
			c.write(" %s", s.Order.Name)
		}
		if s.Nulls != nil {
			c.write(" nulls %s", s.Nulls.Name)
		}
	}
}

func (c *canon) opArgs(args []ast.OpArg) {
	if len(args) == 0 {
		return
	}
	c.write(" (")
	for _, arg := range args {
		switch arg := arg.(type) {
		case *ast.ArgText:
			c.write(" %s %s", arg.Key, sup.QuotedName(arg.Value.Text))
		case *ast.ArgExpr:
			c.write(" %s ", arg.Key)
			c.expr(arg.Value, "")
		default:
			panic("fromArgs")
		}
	}
	c.write(" )")
}

func isAggFunc(e ast.Expr) *ast.AggregateOp {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return nil
	}
	name, ok := call.Func.(*ast.FuncNameExpr)
	if !ok {
		return nil
	}
	if _, err := agg.NewPattern(nil, name.Name, false, true); err != nil {
		return nil
	}
	return &ast.AggregateOp{
		Kind: "AggregateOp",
		Aggs: []ast.Assignment{{
			RHS: call,
		}},
	}
}

func IsBool(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.Primitive:
		return e.Type == "bool"
	case *ast.UnaryExpr:
		return IsBool(e.Operand)
	case *ast.BinaryExpr:
		switch e.Op {
		case "and", "or", "in", "==", "!=", "<", "<=", ">", ">=":
			return true
		default:
			return false
		}
	case *ast.CondExpr:
		return IsBool(e.Then) && IsBool(e.Else)
	case *ast.CallExpr:
		name, ok := e.Func.(*ast.FuncNameExpr)
		return ok && function.HasBoolResult(name.Name)
	case *ast.RegexpExpr, *ast.GlobExpr:
		return true
	default:
		return false
	}
}

func isAggAssignments(assigns []ast.Assignment) bool {
	return !slices.ContainsFunc(assigns, func(a ast.Assignment) bool {
		return isAggFunc(a.RHS) == nil
	})
}

func IsSearch(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.RegexpExpr, *ast.GlobExpr, *ast.SearchTermExpr:
		return true
	case *ast.BinaryExpr:
		switch e.Op {
		case "and", "or":
			return IsSearch(e.LHS) || IsSearch(e.RHS)
		default:
			return false
		}
	case *ast.UnaryExpr:
		return IsSearch(e.Operand)
	default:
		return false
	}
}
