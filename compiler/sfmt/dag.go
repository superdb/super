package sfmt

import (
	"slices"
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/sup"
)

func DAG(main *dag.Main) string {
	d := &canonDAG{
		shared: shared{formatter{tab: 2}},
		head:   true,
		first:  true,
	}
	d.main(main)
	d.flush()
	return d.String()
}

func DAGSeq(seq dag.Seq) string {
	return DAG(&dag.Main{Body: seq})
}

func DAGExpr(e dag.Expr) string {
	d := &canonDAG{
		shared: shared{formatter{tab: 2}},
		head:   true,
		first:  true,
	}
	d.expr(e, "")
	d.flush()
	return d.String()
}

type canonDAG struct {
	shared
	head  bool
	first bool
}

func (c *canonDAG) assignments(assignments []dag.Assignment) {
	for k, a := range assignments {
		if k > 0 {
			c.write(",")
		}
		if a.LHS != nil {
			c.expr(a.LHS, "")
			c.write(":=")
		}
		c.expr(a.RHS, "")
	}
}

func (c *canonDAG) exprs(exprs []dag.Expr) {
	for k, e := range exprs {
		if k > 0 {
			c.write(", ")
		}
		c.expr(e, "")
	}
}

func (c *canonDAG) expr(e dag.Expr, parent string) {
	switch e := e.(type) {
	case nil:
		c.write("null")
	case *dag.AggExpr:
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
	case *dag.ArrayExpr:
		c.write("[")
		c.vectorElems(e.Elems)
		c.write("]")
	case *dag.DotExpr:
		c.expr(e.LHS, "")
		c.write("[%q]", e.RHS)

	case *dag.BinaryExpr:
		c.binary(e, parent)
	case *dag.CondExpr:
		c.write("(")
		c.expr(e.Cond, "")
		c.write(") ? ")
		c.expr(e.Then, "")
		c.write(" : ")
		c.expr(e.Else, "")
	case *dag.CallExpr:
		c.write(e.Tag)
		c.write("(")
		c.exprs(e.Args)
		c.write(")")
	case *dag.IndexExpr:
		c.expr(e.Expr, "")
		c.write("[")
		c.expr(e.Index, "")
		c.write("]")
	case *dag.IsNullExpr:
		c.expr(e.Expr, "")
		c.write(" IS NULL")
	case *dag.MapExpr:
		c.write("map{")
		for k, e := range e.Entries {
			if k > 0 {
				c.write(",")
			}
			c.expr(e.Key, "")
			c.write(":")
			c.expr(e.Value, "")
		}
		c.write("}")
	case *dag.PrimitiveExpr:
		c.write("%s", e.Value)
	case *dag.RecordExpr:
		c.write("{")
		c.recordElems(e.Elems)
		c.write("}")
	case *dag.RegexpSearchExpr:
		c.write("regexp_search(r\"")
		c.write(e.Pattern)
		c.write("\", ")
		c.expr(e.Expr, "")
		c.write(")")
	case *dag.RegexpMatchExpr:
		c.write("regexp_match(/")
		c.write(e.Pattern)
		c.write("/, ")
		c.expr(e.Expr, "")
		c.write(")")
	case *dag.SearchExpr:
		c.write("search(%s)", e.Value)
	case *dag.SetExpr:
		c.write("set[")
		c.vectorElems(e.Elems)
		c.write("]")
	case *dag.SliceExpr:
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
	case *dag.SubqueryExpr:
		c.open("(")
		if e.Correlated {
			c.head = true
			c.next()
			c.write("-- correlated")
		}
		c.head = true
		c.seq(e.Body)
		c.close()
		c.write(")")
	case *dag.ThisExpr:
		c.fieldchain(e.Chain)
	case *dag.TypeExpr:
		if typ, err := super.LookupPrimitiveByID(e.ID); err == nil {
			c.write("<%s>", super.PrimitiveName(typ))
		} else {
			c.write("<id:%d>", e.ID)
		}
	case *dag.UnaryExpr:
		if isnull, ok := e.Operand.(*dag.IsNullExpr); ok && e.Op == "!" {
			c.expr(isnull.Expr, "")
			c.write(" IS NOT NULL")
		} else {
			c.write(e.Op)
			c.expr(e.Operand, "not")
		}
	default:
		c.open("(unknown expr %T)", e)
		c.close()
		c.ret()
	}
}

func (c *canonDAG) binary(e *dag.BinaryExpr, parent string) {
	switch e.Op {
	case ".":
		if !isDAGThis(e.LHS) {
			c.expr(e.LHS, "")
			c.write(".")
		}
		c.expr(e.RHS, "")
	case "in", "and", "or":
		parens := needsparens(parent, e.Op)
		c.maybewrite("(", parens)
		c.expr(e.LHS, e.Op)
		c.write(" %s ", e.Op)
		c.expr(e.RHS, e.Op)
		c.maybewrite(")", parens)
	default:
		parens := needsparens(parent, e.Op)
		c.maybewrite("(", parens)
		c.expr(e.LHS, e.Op)
		c.write("%s", e.Op)
		c.expr(e.RHS, e.Op)
		c.maybewrite(")", parens)
	}
}

func (c *canonDAG) recordElems(elems []dag.RecordElem) {
	for k, elem := range elems {
		if k > 0 {
			c.write(",")
		}
		switch e := elem.(type) {
		case *dag.Field:
			c.write(sup.QuotedName(e.Name))
			c.write(":")
			c.expr(e.Value, "")
		case *dag.Spread:
			c.write("...")
			c.expr(e.Expr, "")
		default:
			panic(e)
		}
	}
}

func (c *canonDAG) vectorElems(elems []dag.VectorElem) {
	for k, elem := range elems {
		if k > 0 {
			c.write(",")
		}
		switch elem := elem.(type) {
		case *dag.Spread:
			c.write("...")
			c.expr(elem.Expr, "")
		case *dag.VectorValue:
			c.expr(elem.Expr, "")
		default:
			c.write("sfmt: unknown vector elem type: %T", elem)
		}
	}
}

func isDAGThis(e dag.Expr) bool {
	if this, ok := e.(*dag.ThisExpr); ok {
		if len(this.Chain) == 0 {
			return true
		}
	}
	return false
}

func (c *canonDAG) maybewrite(s string, do bool) {
	if do {
		c.write(s)
	}
}

func (c *canonDAG) next() {
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

func (c *canonDAG) seq(seq dag.Seq) {
	for _, p := range seq {
		c.op(p)
	}
}

func (c *canonDAG) op(o dag.Op) {
	switch o := o.(type) {
	//
	// Scanners in alphabetical order.
	//
	case *dag.CommitMetaScan:
		c.next()
		c.write("table %s@%s:%s", o.Table, o.Commit, o.Meta)
		if o.Tap {
			c.write(" tap")
		}
	case *dag.DBMetaScan:
		c.next()
		c.write(":%s", o.Meta)
	case *dag.FileScan:
		c.next()
		c.write("file ")
		for i, p := range o.Paths {
			if i > 0 {
				c.write(", ")
			}
			c.write(p)
			if f := o.Formats[i]; f != "" {
				c.write(" format %s", f)
			}
		}
		if o.Type != "" {
			c.write(" type %s", o.Type)
		}
		if o.Pushdown.Unordered {
			c.write(" unordered")
		}
		if o.Pushdown.HasProjection {
			c.project(o.Pushdown.Projection)
		}
		c.scanFilter("meta-filter", o.Pushdown.MetaFilter)
		c.scanFilter("search-filter", o.Pushdown.SearchFilter)
		c.scanFilter("data-filter", o.Pushdown.DataFilter)
	case *dag.HTTPScan:
		c.next()
		if o.Method != "" && !strings.EqualFold(o.Method, "GET") {
			c.write("%s %s", strings.ToLower(o.Method), o.URL)
		} else {
			c.write("get %s", o.URL)
		}
		if o.Format != "" {
			c.write(" format %s", o.Format)
		}
		if o.Type != "" {
			c.write(" type %s", o.Type)
		}
	case *dag.ListerScan:
		c.next()
		c.open("lister")
		c.write(" table %s commit %s", o.Table, o.Commit)
		if o.KeyPruner != nil {
			c.write(" pruner (")
			c.expr(o.KeyPruner, "")
			c.write(")")
		}
		c.close()
	case *dag.NullScan:
		c.next()
		c.write("null")
	case *dag.RobotScan:
		c.next()
		c.write("robot ")
		c.expr(o.Expr, "")
		if o.Format != "" {
			c.write(" format %s", o.Format)
		}
		if o.Filter != nil {
			c.write(" filter (")
			c.expr(o.Filter, "")
			c.write(")")
		}
	case *dag.SeqScan:
		c.next()
		c.open("seqscan")
		c.write(" table %s", o.Table)
		if o.KeyPruner != nil {
			c.write(" pruner (")
			c.expr(o.KeyPruner, "")
			c.write(")")
		}
		if len(o.Fields) > 0 {
			c.project(o.Fields)
		}
		if o.Filter != nil {
			c.write(" filter (")
			c.expr(o.Filter, "")
			c.write(")")
		}
		c.close()
	case *dag.TableScan:
		c.next()
		c.write("table %s", o.ID)
		if o.Type != "" {
			c.write(" type %s", o.Type)
		}
	case *dag.TableMetaScan:
		c.next()
		c.write("table %s:%s", o.ID, o.Meta)
	//
	// Operators in alphabeticl order.
	//
	case *dag.AggregateOp:
		c.next()
		c.open("aggregate")
		if o.PartialsIn {
			c.write(" partials-in")
		}
		if o.PartialsOut {
			c.write(" partials-out")
		}
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
	case *dag.CombineOp:
		c.next()
		c.write("combine")
	case *dag.CountOp:
		c.next()
		c.write("count {")
		if o.Expr != nil {
			c.recordElems(o.Expr.(*dag.RecordExpr).Elems)
			c.write(",")
		}
		c.write("%s}", o.Alias)
	case *dag.DebugOp:
		c.next()
		c.write("debug ")
		c.expr(o.Expr, "")
		if o.Filter != nil {
			c.write(" filter (")
			c.expr(o.Filter, "filter")
			c.write(")")
		}
	case *dag.DistinctOp:
		c.next()
		c.write("distinct ")
		c.expr(o.Expr, "")
	case *dag.DropOp:
		c.next()
		c.write("drop ")
		c.exprs(o.Args)
	case *dag.FilterOp:
		c.next()
		c.open("where ")
		c.expr(o.Expr, "")
		c.close()
	case *dag.ForkOp:
		c.next()
		c.open("fork")
		for _, seq := range o.Paths {
			c.ret()
			c.write("(")
			c.open()
			c.head = true
			c.seq(seq)
			c.close()
			c.ret()
			c.write(")")
		}
		c.close()
		c.flush()
	case *dag.FuseOp:
		c.next()
		if o.Complete {
			c.write("superfuse")
		} else {
			c.write("fuse")
		}
	case *dag.HashJoinOp:
		c.next()
		c.write("%s hashjoin as {%s,%s} on ", o.Style, o.LeftAlias, o.RightAlias)
		c.expr(o.LeftKey, "")
		c.write("==")
		c.expr(o.RightKey, "")
	case *dag.HeadOp:
		c.next()
		c.write("head %d", o.Count)
	case *dag.JoinOp:
		c.next()
		c.open()
		c.write("%s join as {%s,%s}", o.Style, o.LeftAlias, o.RightAlias)
		if o.Cond != nil {
			c.write(" on ")
			c.expr(o.Cond, "")
		}
		c.close()
	case *dag.LoadOp:
		c.next()
		c.write("load %s", o.Table)
		if o.Branch != "" {
			c.write("@%s", o.Branch)
		}
		if o.Author != "" {
			c.write(" author %s", o.Author)
		}
		if o.Message != "" {
			c.write(" message %s", o.Message)
		}
		if o.Meta != "" {
			c.write(" meta %s", o.Meta)
		}
	case *dag.MergeOp:
		c.next()
		c.write("merge")
		c.sortExprs(o.Exprs)
	case *dag.OutputOp:
		c.next()
		c.write("output %s", o.Name)
	case *dag.PassOp:
		c.next()
		c.write("pass")
	case *dag.PutOp:
		c.next()
		c.write("put ")
		c.assignments(o.Args)
	case *dag.RenameOp:
		c.next()
		c.write("rename ")
		c.assignments(o.Args)
	case *dag.ScatterOp:
		c.next()
		c.open("scatter")
		for _, seq := range o.Paths {
			c.ret()
			c.write("(")
			c.open()
			c.head = true
			c.seq(seq)
			c.close()
			c.ret()
			c.write(")")
		}
		c.close()
		c.flush()
	case *dag.SkipOp:
		c.next()
		c.write("skip %d", o.Count)
	case *dag.SlicerOp:
		c.next()
		c.open("slicer")
		c.close()
	case *dag.SortOp:
		c.next()
		c.write("sort")
		if o.Reverse {
			c.write(" -r")
		}
		c.sortExprs(o.Exprs)
	case *dag.SwitchOp:
		c.next()
		c.open("switch")
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
	case *dag.TailOp:
		c.next()
		c.write("tail %d", o.Count)
	case *dag.TopOp:
		c.next()
		c.write("top")
		if o.Reverse {
			c.write(" -r")
		}
		c.write(" %d", o.Limit)
		c.sortExprs(o.Exprs)
	case *dag.UniqOp:
		c.next()
		c.write("uniq")
		if o.Cflag {
			c.write(" -c")
		}
	case *dag.UnnestOp:
		c.unnest(o)
	case *dag.ValuesOp:
		c.next()
		c.write("values ")
		c.exprs(o.Exprs)
	default:
		c.next()
		c.open("unknown operator: %T", o)
		c.close()
	}
}

func (c *canonDAG) scanFilter(which string, sf *dag.ScanFilter) {
	if sf != nil && sf.Expr != nil {
		c.ret()
		c.open()
		c.open(which)
		c.project(sf.Projection)
		c.write(" (")
		c.ret()
		c.expr(sf.Expr, "")
		c.ret()
		c.close()
		c.ret()
		c.write(")")
		c.close()
	}
}

func (c *canonDAG) project(fields []field.Path) {
	var ss []string
	for _, f := range fields {
		ss = append(ss, f.String())
	}
	slices.Sort(ss)
	c.write(" project [%s]", strings.Join(ss, ","))
}

func (c *canonDAG) unnest(u *dag.UnnestOp) {
	c.next()
	c.write("unnest ")
	c.expr(u.Expr, "")
}

func (c *canonDAG) main(main *dag.Main) {
	for _, f := range main.Funcs {
		c.write("fn %s/%s(", f.Tag, f.Name)
		for i, p := range f.Params {
			if i != 0 {
				c.write(", ")
			}
			c.write(p)
		}
		c.open("): (")
		c.ret()
		c.expr(f.Expr, "")
		c.close()
		c.ret()
		c.flush()
		c.write(")")
		c.ret()
		c.flush()
	}
	c.head = true
	c.seq(main.Body)
}

func (c *canonDAG) sortExprs(sortExprs []dag.SortExpr) {
	for i, s := range sortExprs {
		if i > 0 {
			c.write(",")
		}
		c.space()
		c.expr(s.Key, "")
		c.write(" %s nulls %s", s.Order, s.Nulls)
	}
}
