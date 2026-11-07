package optimizer

import (
	"slices"

	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/compiler/optimizer/demand"
)

type demandTable map[dag.Op]demand.Demand

// analyzeDemand computes the demandTable for seq given that all of its
// output is needed.  It does not modify seq.
func analyzeDemand(seq dag.Seq) demandTable {
	a := &demandAnalyzer{table: make(demandTable)}
	a.demandForSeq(seq, demand.All())
	return a.table
}

func DemandForSeq(seq dag.Seq, downstreams ...demand.Demand) []demand.Demand {
	a := &demandAnalyzer{table: make(demandTable)}
	return a.demandForSeq(seq, downstreams...)
}

type demandAnalyzer struct {
	table demandTable
}

func (a *demandAnalyzer) demandForSeq(seq dag.Seq, downstreams ...demand.Demand) []demand.Demand {
	for _, op := range slices.Backward(seq) {
		downstreams = a.demandForOp(op, downstreams)
	}
	return downstreams
}

func (a *demandAnalyzer) demandForOp(op dag.Op, downstreams []demand.Demand) []demand.Demand {
	a.table[op] = demand.Union(downstreams...)
	switch op := op.(type) {
	case *dag.ForkOp:
		var out []demand.Demand
		for i, p := range op.Paths {
			i := min(i, len(downstreams)-1)
			out = append(out, demand.Union(a.demandForSeq(p, downstreams[i])...))
		}
		return out
	case *dag.HashJoinOp:
		downstream := downstreams[0]
		left := demand.GetKey(downstream, op.LeftAlias)
		left = demand.Union(left, a.demandForExpr(op.LeftKey))
		right := demand.GetKey(downstream, op.RightAlias)
		right = demand.Union(right, a.demandForExpr(op.RightKey))
		return []demand.Demand{left, right}
	case *dag.JoinOp:
		d := demand.Union(downstreams[0], a.demandForExpr(op.Cond))
		left := demand.GetKey(d, op.LeftAlias)
		right := demand.GetKey(d, op.RightAlias)
		return []demand.Demand{left, right}
	case *dag.ScatterOp:
		d := demand.None()
		for i, p := range op.Paths {
			i := min(i, len(downstreams)-1)
			d = demand.Union(d, demand.Union(a.demandForSeq(p, downstreams[i])...))
		}
		return []demand.Demand{d}
	case *dag.SwitchOp:
		d := a.demandForExpr(op.Expr)
		for i, c := range op.Cases {
			d = demand.Union(d, a.demandForExpr(c.Expr))
			i := min(i, len(downstreams)-1)
			d = demand.Union(d, demand.Union(a.demandForSeq(c.Path, downstreams[i])...))
		}
		return []demand.Demand{d}
	default:
		return []demand.Demand{a.demandForSimpleOp(op, demand.Union(downstreams...))}
	}
}

func (a *demandAnalyzer) demandForSimpleOp(op dag.Op, downstream demand.Demand) demand.Demand {
	switch op := op.(type) {
	case *dag.AggregateOp:
		d := demand.None()
		for _, assignment := range op.Keys {
			d = demand.Union(d, a.demandForExpr(assignment.RHS))
		}
		for _, assignment := range op.Aggs {
			d = demand.Union(d, a.demandForExpr(assignment.RHS))
		}
		return d
	case *dag.CombineOp:
		return downstream
	case *dag.CountOp:
		return demand.Union(downstream, a.demandForExpr(op.Expr))
	case *dag.DebugOp:
		d := demand.Union(downstream, a.demandForExpr(op.Expr))
		return demand.Union(d, a.demandForExpr(op.Filter))
	case *dag.DistinctOp:
		return demand.Union(downstream, a.demandForExpr(op.Expr))
	case *dag.DropOp:
		return downstream
	case *dag.FilterOp:
		return demand.Union(downstream, a.demandForExpr(op.Expr))
	case *dag.FuseOp:
		return demand.All()
	case *dag.HeadOp, *dag.InferOp:
		return downstream
	case *dag.LoadOp:
		return demand.All()
	case *dag.MergeOp:
		return a.demandForSortExprs(op.Exprs, downstream)
	case *dag.OutputOp:
		return demand.All()
	case *dag.PassOp:
		return downstream
	case *dag.PutOp:
		return a.demandForAssignments(op.Args, downstream)
	case *dag.RenameOp:
		return a.demandForAssignments(op.Args, downstream)
	case *dag.SkipOp:
		return downstream
	case *dag.SortOp:
		return a.demandForSortExprs(op.Exprs, downstream)
	case *dag.TailOp:
		return downstream
	case *dag.TopOp:
		return a.demandForSortExprs(op.Exprs, downstream)
	case *dag.UniqOp:
		// Uniq compares entire values.
		return demand.All()
	case *dag.UnnestOp:
		return a.demandForExpr(op.Expr)
	case *dag.ValuesOp:
		d := demand.None()
		for _, e := range op.Exprs {
			d = demand.Union(d, a.demandForExpr(e))
		}
		return d

	case *dag.CommitMetaScan, *dag.DeleterScan, *dag.DeleteScan, *dag.DBMetaScan:
		return demand.None()
	case *dag.FileScan:
		// Traverse the pushdown filters so any subqueries within them
		// are entered into the table.
		if mf := op.Pushdown.MetaFilter; mf != nil {
			a.demandForExpr(mf.Expr)
		}
		if df := op.Pushdown.DataFilter; df != nil {
			a.demandForExpr(df.Expr)
		}
		return demand.None()
	case *dag.HTTPScan, *dag.ListerScan, *dag.NullScan, *dag.TableMetaScan, *dag.TableScan:
		return demand.None()
	case *dag.RobotScan:
		return demand.Union(a.demandForExpr(op.Expr), a.demandForExpr(op.Method), a.demandForExpr(op.Headers), a.demandForExpr(op.Body))
	case *dag.SeqScan:
		a.demandForExpr(op.Filter)
		a.demandForExpr(op.KeyPruner)
		return demand.None()
	case *dag.SlicerOp:
		return demand.None()
	}
	panic(op)
}

func (a *demandAnalyzer) demandForExpr(expr dag.Expr) demand.Demand {
	switch expr := expr.(type) {
	case nil:
		return demand.None()
	case *dag.AggExpr:
		return demand.Union(a.demandForExpr(expr.Expr), a.demandForExpr(expr.Filter))
	case *dag.ArrayExpr:
		return a.demandForArrayOrSetExpr(expr.Elems)
	case *dag.BinaryExpr:
		return demand.Union(a.demandForExpr(expr.LHS), a.demandForExpr(expr.RHS))
	case *dag.CallExpr:
		d := demand.None()
		for _, arg := range expr.Args {
			d = demand.Union(d, a.demandForExpr(arg))
		}
		return d
	case *dag.CondExpr:
		return demand.Union(a.demandForExpr(expr.Cond),
			demand.Union(a.demandForExpr(expr.Then), a.demandForExpr(expr.Else)))
	case *dag.DotExpr:
		return a.demandForExpr(expr.LHS)
	case *dag.IndexExpr:
		return demand.Union(a.demandForExpr(expr.Expr), a.demandForExpr(expr.Index))
	case *dag.IsNullExpr:
		return a.demandForExpr(expr.Expr)
	case *dag.TypeAssertExpr:
		return demand.Union(a.demandForExpr(expr.Expr), a.demandForExpr(expr.Type))
	case *dag.MatchExpr:
		// The results are evaluated on their inputs, not on this, so only
		// the inputs (and the matched value and case types) demand fields
		// of this.
		d := demand.Union(a.demandForExpr(expr.Expr), a.demandForExpr(expr.ElseInput))
		for _, c := range expr.Cases {
			d = demand.Union(d, a.demandForExpr(c.Type), a.demandForExpr(c.Input))
		}
		return d
	case *dag.PrimitiveExpr, *dag.TypeExpr:
		return demand.None()
	case *dag.MapCallExpr:
		return a.demandForExpr(expr.Expr)
	case *dag.MapExpr:
		d := demand.None()
		for _, e := range expr.Entries {
			d = demand.Union(d, a.demandForExpr(e.Key))
			d = demand.Union(d, a.demandForExpr(e.Value))
		}
		return d
	case *dag.RecordExpr:
		d := demand.None()
		for _, e := range expr.Elems {
			switch e := e.(type) {
			case *dag.Field:
				d = demand.Union(d, a.demandForExpr(e.Value))
			case *dag.Spread:
				d = demand.Union(d, a.demandForExpr(e.Expr))
			default:
				panic(e)
			}
		}
		return d
	case *dag.RegexpMatchExpr:
		return a.demandForExpr(expr.Expr)
	case *dag.RegexpSearchExpr:
		return a.demandForExpr(expr.Expr)
	case *dag.SearchExpr:
		return a.demandForExpr(expr.Expr)
	case *dag.SetExpr:
		return a.demandForArrayOrSetExpr(expr.Elems)
	case *dag.SliceExpr:
		return demand.Union(a.demandForExpr(expr.Expr),
			demand.Union(a.demandForExpr(expr.From), a.demandForExpr(expr.To)))
	case *dag.SubqueryExpr:
		return demand.Union(a.demandForSeq(expr.Body, demand.All())...)
	case *dag.ThisExpr:
		d := demand.All()
		for _, elem := range slices.Backward(expr.Chain) {
			d = demand.Key(elem.ID, d)
		}
		return d
	case *dag.UnaryExpr:
		return a.demandForExpr(expr.Operand)
	}
	panic(expr)
}

func (a *demandAnalyzer) demandForArrayOrSetExpr(elems []dag.VectorElem) demand.Demand {
	d := demand.None()
	for _, e := range elems {
		switch e := e.(type) {
		case *dag.Spread:
			d = demand.Union(d, a.demandForExpr(e.Expr))
		case *dag.VectorValue:
			d = demand.Union(d, a.demandForExpr(e.Expr))
		default:
			panic(e)
		}
	}
	return d
}

func (a *demandAnalyzer) demandForAssignments(assignments []dag.Assignment, downstream demand.Demand) demand.Demand {
	d := downstream
	for _, assign := range assignments {
		if _, ok := assign.LHS.(*dag.ThisExpr); ok {
			// Assignment clobbers a static field.
			d = demand.Delete(d, a.demandForExpr(assign.LHS))
		} else {
			// Add anything needed by a dynamic field.
			d = demand.Union(d, a.demandForExpr(assign.LHS))
		}
		d = demand.Union(d, a.demandForExpr(assign.RHS))
	}
	return d
}

func (a *demandAnalyzer) demandForSortExprs(sortExprs []dag.SortExpr, downstream demand.Demand) demand.Demand {
	if len(sortExprs) == 0 {
		// Need all fields to guess sort key.
		return demand.All()
	}
	d := downstream
	for _, s := range sortExprs {
		d = demand.Union(d, a.demandForExpr(s.Key))
	}
	return d
}
