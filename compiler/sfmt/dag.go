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

func (c *canonDAG) op(p dag.Op) {
	switch p := p.(type) {
	//
	// Scanners in alphabetical order.
	//
	case *dag.CommitMetaScan:
		c.next()
		c.write("pool %s@%s:%s", p.Pool, p.Commit, p.Meta)
		if p.Tap {
			c.write(" tap")
		}
	case *dag.DBMetaScan:
		c.next()
		c.write(":%s", p.Meta)
	case *dag.FileScan:
		c.next()
		c.write("file %s", strings.Join(p.Paths, ","))
		if p.Format != "" {
			c.write(" format %s", p.Format)
		}
		if p.Type != "" {
			c.write(" type %s", p.Type)
		}
		if p.Pushdown.Unordered {
			c.write(" unordered")
		}
		if len(p.Pushdown.Projection) > 0 || p.Pushdown.None {
			c.project(p.Pushdown.Projection, p.Pushdown.None)
		}
		if mf := p.Pushdown.MetaFilter; mf != nil {
			if mf.Expr != nil {
				c.ret()
				c.open()
				c.open("meta-filter")
				c.project(mf.Projection, false)
				c.write(" (")
				c.ret()
				c.expr(mf.Expr, "")
				c.ret()
				c.close()
				c.ret()
				c.write(")")
				c.close()
			}
		}
		if df := p.Pushdown.DataFilter; df != nil {
			if df.Expr != nil {
				c.ret()
				c.open()
				c.open("data-filter")
				c.project(df.Projection, false)
				c.write(" (")
				c.ret()
				c.expr(df.Expr, "")
				c.ret()
				c.close()
				c.ret()
				c.write(")")
				c.close()
			}
		}
	case *dag.HTTPScan:
		c.next()
		c.write("get %s", p.URL)
	case *dag.ListerScan:
		c.next()
		c.open("lister")
		c.write(" pool %s commit %s", p.Pool, p.Commit)
		if p.KeyPruner != nil {
			c.write(" pruner (")
			c.expr(p.KeyPruner, "")
			c.write(")")
		}
		c.close()
	case *dag.NullScan:
		c.next()
		c.write("null")
	case *dag.PoolScan:
		c.next()
		c.write("pool %s", p.ID)
	case *dag.PoolMetaScan:
		c.next()
		c.write("pool %s:%s", p.ID, p.Meta)
	case *dag.RobotScan:
		c.next()
		c.write("robot ")
		c.expr(p.Expr, "")
		if p.Format != "" {
			c.write(" format %s", p.Format)
		}
		if p.Filter != nil {
			c.write(" filter (")
			c.expr(p.Filter, "")
			c.write(")")
		}
	case *dag.SeqScan:
		c.next()
		c.open("seqscan")
		c.write(" pool %s", p.Pool)
		if p.KeyPruner != nil {
			c.write(" pruner (")
			c.expr(p.KeyPruner, "")
			c.write(")")
		}
		if len(p.Fields) > 0 {
			c.project(p.Fields, false)
		}
		if p.Filter != nil {
			c.write(" filter (")
			c.expr(p.Filter, "")
			c.write(")")
		}
		c.close()
	//
	// Operators in alphabeticl order.
	//
	case *dag.AggregateOp:
		c.next()
		c.open("aggregate")
		if p.PartialsIn {
			c.write(" partials-in")
		}
		if p.PartialsOut {
			c.write(" partials-out")
		}
		c.ret()
		c.open()
		c.assignments(p.Aggs)
		if len(p.Keys) != 0 {
			c.write(" by ")
			c.assignments(p.Keys)
		}
		if p.Limit != 0 {
			c.write(" -with limit %d", p.Limit)
		}
		c.close()
		c.close()
	case *dag.CombineOp:
		c.next()
		c.write("combine")
	case *dag.CountOp:
		c.next()
		c.write("count {")
		if p.Expr != nil {
			c.recordElems(p.Expr.(*dag.RecordExpr).Elems)
			c.write(",")
		}
		c.write("%s}", p.Alias)
	case *dag.CutOp:
		c.next()
		c.write("cut ")
		c.assignments(p.Args)
	case *dag.DebugOp:
		c.next()
		c.write("debug ")
		c.expr(p.Expr, "")
		if p.Filter != nil {
			c.write(" filter (")
			c.expr(p.Filter, "filter")
			c.write(")")
		}
	case *dag.DistinctOp:
		c.next()
		c.write("distinct ")
		c.expr(p.Expr, "")
	case *dag.DropOp:
		c.next()
		c.write("drop ")
		c.exprs(p.Args)
	case *dag.FilterOp:
		c.next()
		c.open("where ")
		c.expr(p.Expr, "")
		c.close()
	case *dag.ForkOp:
		c.next()
		c.open("fork")
		for _, seq := range p.Paths {
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
		c.write("fuse")
	case *dag.HashJoinOp:
		c.next()
		c.write("%s hashjoin as {%s,%s} on ", p.Style, p.LeftAlias, p.RightAlias)
		c.expr(p.LeftKey, "")
		c.write("==")
		c.expr(p.RightKey, "")
	case *dag.HeadOp:
		c.next()
		c.write("head %d", p.Count)
	case *dag.JoinOp:
		c.next()
		c.open()
		c.write("%s join as {%s,%s}", p.Style, p.LeftAlias, p.RightAlias)
		if p.Cond != nil {
			c.write(" on ")
			c.expr(p.Cond, "")
		}
		c.close()
	case *dag.LoadOp:
		c.next()
		c.write("load %s", p.Pool)
		if p.Branch != "" {
			c.write("@%s", p.Branch)
		}
		if p.Author != "" {
			c.write(" author %s", p.Author)
		}
		if p.Message != "" {
			c.write(" message %s", p.Message)
		}
		if p.Meta != "" {
			c.write(" meta %s", p.Meta)
		}
	case *dag.MergeOp:
		c.next()
		c.write("merge")
		c.sortExprs(p.Exprs)
	case *dag.OutputOp:
		c.next()
		c.write("output %s", p.Name)
	case *dag.PassOp:
		c.next()
		c.write("pass")
	case *dag.PutOp:
		c.next()
		c.write("put ")
		c.assignments(p.Args)
	case *dag.RenameOp:
		c.next()
		c.write("rename ")
		c.assignments(p.Args)
	case *dag.ScatterOp:
		c.next()
		c.open("scatter")
		for _, seq := range p.Paths {
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
		c.write("skip %d", p.Count)
	case *dag.SlicerOp:
		c.next()
		c.open("slicer")
		c.close()
	case *dag.SortOp:
		c.next()
		c.write("sort")
		if p.Reverse {
			c.write(" -r")
		}
		c.sortExprs(p.Exprs)
	case *dag.SwitchOp:
		c.next()
		c.open("switch")
		if p.Expr != nil {
			c.write(" ")
			c.expr(p.Expr, "")
		}
		for _, k := range p.Cases {
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
		c.write("tail %d", p.Count)
	case *dag.TopOp:
		c.next()
		c.write("top")
		if p.Reverse {
			c.write(" -r")
		}
		c.write(" %d", p.Limit)
		c.sortExprs(p.Exprs)
	case *dag.UniqOp:
		c.next()
		c.write("uniq")
		if p.Cflag {
			c.write(" -c")
		}
	case *dag.UnnestOp:
		c.unnest(p)
	case *dag.ValuesOp:
		c.next()
		c.write("values ")
		c.exprs(p.Exprs)
	default:
		c.next()
		c.open("unknown operator: %T", p)
		c.close()
	}
}

func (c *canonDAG) project(fields []field.Path, none bool) {
	if len(fields) == 0 {
		if none {
			c.write(" project none")
		}
		return
	}
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
