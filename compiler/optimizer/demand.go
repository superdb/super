package optimizer

import (
	"slices"

	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/compiler/optimizer/demand"
)

func DemandForSeq(seq dag.Seq, downstreams ...demand.Demand) []demand.Demand {
	for _, op := range slices.Backward(seq) {
		downstreams = demandForOp(op, downstreams)
	}
	return downstreams
}

func demandForOp(op dag.Op, downstreams []demand.Demand) []demand.Demand {
	switch op := op.(type) {
	case *dag.ForkOp:
		var out []demand.Demand
		for i, p := range op.Paths {
			i := min(i, len(downstreams)-1)
			out = append(out, demand.Union(DemandForSeq(p, downstreams[i])...))
		}
		return out
	case *dag.HashJoinOp:
		downstream := materialized(downstreams[0])
		left := demand.GetKey(downstream, op.LeftAlias)
		left = demand.Union(left, demandForExpr(op.LeftKey))
		right := demand.GetKey(downstream, op.RightAlias)
		right = demand.Union(right, demandForExpr(op.RightKey))
		return []demand.Demand{left, right}
	case *dag.JoinOp:
		d := demand.Union(downstreams[0], demandForExpr(op.Cond))
		left := demand.GetKey(d, op.LeftAlias)
		right := demand.GetKey(d, op.RightAlias)
		return []demand.Demand{left, right}
	case *dag.ScatterOp:
		d := demand.None()
		for i, p := range op.Paths {
			i := min(i, len(downstreams)-1)
			d = demand.Union(d, demand.Union(DemandForSeq(p, downstreams[i])...))
		}
		return []demand.Demand{d}
	case *dag.SwitchOp:
		d := demandForExpr(op.Expr)
		for i, c := range op.Cases {
			d = demand.Union(d, demandForExpr(c.Expr))
			i := min(i, len(downstreams)-1)
			d = demand.Union(d, demand.Union(DemandForSeq(c.Path, downstreams[i])...))
		}
		return []demand.Demand{d}
	default:
		return []demand.Demand{demandForSimpleOp(op, demand.Union(downstreams...))}
	}
}

func demandForSimpleOp(op dag.Op, downstream demand.Demand) demand.Demand {
	switch op := op.(type) {
	case *dag.AggregateOp:
		d := demand.None()
		for _, assignment := range op.Keys {
			d = demand.Union(d, demandForExpr(assignment.RHS))
		}
		for _, assignment := range op.Aggs {
			d = demand.Union(d, demandForExpr(assignment.RHS))
		}
		return d
	case *dag.CombineOp:
		return downstream
	case *dag.CountOp:
		return demand.Union(downstream, demandForExpr(op.Expr))
	case *dag.DebugOp:
		d := demand.Union(downstream, demandForExpr(op.Expr))
		return demand.Union(d, demandForExpr(op.Filter))
	case *dag.DistinctOp:
		return demand.Union(downstream, demandForExpr(op.Expr))
	case *dag.DropOp:
		return downstream
	case *dag.FilterOp:
		return demand.Union(downstream, demandForExpr(op.Expr))
	case *dag.FuseOp:
		return demand.All()
	case *dag.HeadOp:
		return downstream
	case *dag.InferOp:
		return materialized(downstream)
	case *dag.LoadOp:
		return demand.All()
	case *dag.MergeOp:
		return demandForSortExprs(op.Exprs, materialized(downstream))
	case *dag.OutputOp:
		return demand.All()
	case *dag.PassOp:
		return downstream
	case *dag.PutOp:
		return demandForAssignments(op.Args, downstream)
	case *dag.RenameOp:
		return demandForAssignments(op.Args, downstream)
	case *dag.SkipOp:
		return downstream
	case *dag.SortOp:
		return demandForSortExprs(op.Exprs, materialized(downstream))
	case *dag.TailOp:
		return downstream
	case *dag.TopOp:
		return demandForSortExprs(op.Exprs, materialized(downstream))
	case *dag.UniqOp:
		return materialized(downstream)
	case *dag.UnnestOp:
		return demandForExpr(op.Expr)
	case *dag.ValuesOp:
		d := demand.None()
		for _, e := range op.Exprs {
			d = demand.Union(d, demandForExpr(e))
		}
		return d

	case *dag.CommitMetaScan, *dag.DeleterScan, *dag.DeleteScan, *dag.DBMetaScan:
		return demand.None()
	case *dag.FileScan:
		if mf := op.Pushdown.MetaFilter; mf != nil {
			mf.HasProjection = true
			mf.Projection = demand.Fields(demand.WithoutTypes(demandForExpr(mf.Expr)))
		}
		if df := op.Pushdown.DataFilter; df != nil {
			exprDemand := demandForExpr(df.Expr)
			if demand.IsAll(exprDemand) {
				// If the predicate wants to look at the entire this, then it is a candidate
				// for search.  We don't check for sure if it is and let the compiler do
				// that later, but this dataflow analysis here lets us put the candidate
				// in place.
				op.Pushdown.SearchFilter = &dag.ScanFilter{Expr: df.Expr}
			} else {
				df.HasProjection = true
				df.Projection = demand.Fields(exprDemand)
				df.TypeProjection = demand.TypeFields(exprDemand)
			}
		}
		op.Pushdown.HasProjection = !demand.IsAll(downstream)
		op.Pushdown.Projection = demand.Fields(downstream)
		op.Pushdown.TypeProjection = demand.TypeFields(downstream)
		return demand.None()
	case *dag.HTTPScan, *dag.ListerScan, *dag.NullScan, *dag.TableMetaScan, *dag.TableScan:
		return demand.None()
	case *dag.RobotScan:
		return demandForExpr(op.Expr)
	case *dag.SeqScan:
		d := demand.Union(downstream, demandForExpr(op.Filter))
		d = demand.Union(d, demandForExpr(op.KeyPruner))
		op.Fields = demand.Fields(demand.WithoutTypes(d))
		return demand.None()
	case *dag.SlicerOp:
		return demand.None()
	}
	panic(op)
}

// materialized returns the demand for the input of an operator that
// materializes the values it passes through.  Their data must be loaded
// even if only their types are needed downstream.
func materialized(downstream demand.Demand) demand.Demand {
	return demand.WithoutTypes(downstream)
}

func demandForExpr(expr dag.Expr) demand.Demand {
	switch expr := expr.(type) {
	case nil:
		return demand.None()
	case *dag.AggExpr:
		return demand.Union(demandForExpr(expr.Expr), demandForExpr(expr.Filter))
	case *dag.ArrayExpr:
		return demandForArrayOrSetExpr(expr.Elems)
	case *dag.BinaryExpr:
		return demand.Union(demandForExpr(expr.LHS), demandForExpr(expr.RHS))
	case *dag.CallExpr:
		if isTypeFunc(expr) {
			if this, ok := expr.Args[0].(*dag.ThisExpr); ok {
				// Only the type of the path is needed.
				d := demand.TypesOnly()
				for _, elem := range slices.Backward(this.Chain) {
					d = demand.Key(elem.ID, d)
				}
				return d
			}
		}
		d := demand.None()
		for _, a := range expr.Args {
			d = demand.Union(d, demandForExpr(a))
		}
		return d
	case *dag.CondExpr:
		return demand.Union(demandForExpr(expr.Cond),
			demand.Union(demandForExpr(expr.Then), demandForExpr(expr.Else)))
	case *dag.DotExpr:
		return demandForExpr(expr.LHS)
	case *dag.IndexExpr:
		return demand.Union(demandForExpr(expr.Expr), demandForExpr(expr.Index))
	case *dag.IsNullExpr:
		return demandForExpr(expr.Expr)
	case *dag.PrimitiveExpr, *dag.TypeExpr:
		return demand.None()
	case *dag.MapCallExpr:
		return demandForExpr(expr.Expr)
	case *dag.MapExpr:
		d := demand.None()
		for _, e := range expr.Entries {
			d = demand.Union(d, demandForExpr(e.Key))
			d = demand.Union(d, demandForExpr(e.Value))
		}
		return d
	case *dag.RecordExpr:
		d := demand.None()
		for _, e := range expr.Elems {
			switch e := e.(type) {
			case *dag.Field:
				d = demand.Union(d, demandForExpr(e.Value))
			case *dag.Spread:
				d = demand.Union(d, demandForExpr(e.Expr))
			default:
				panic(e)
			}
		}
		return d
	case *dag.RegexpMatchExpr:
		return demandForExpr(expr.Expr)
	case *dag.RegexpSearchExpr:
		return demandForExpr(expr.Expr)
	case *dag.SearchExpr:
		return demandForExpr(expr.Expr)
	case *dag.SetExpr:
		return demandForArrayOrSetExpr(expr.Elems)
	case *dag.SliceExpr:
		return demand.Union(demandForExpr(expr.Expr),
			demand.Union(demandForExpr(expr.From), demandForExpr(expr.To)))
	case *dag.SubqueryExpr:
		return demand.Union(DemandForSeq(expr.Body, demand.All())...)
	case *dag.ThisExpr:
		d := demand.All()
		for _, elem := range slices.Backward(expr.Chain) {
			d = demand.Key(elem.ID, d)
		}
		return d
	case *dag.UnaryExpr:
		return demandForExpr(expr.Operand)
	}
	panic(expr)
}

// isTypeFunc returns true if call is a call to a built-in function whose
// result depends only on the type of its argument.
func isTypeFunc(call *dag.CallExpr) bool {
	if len(call.Args) != 1 {
		return false
	}
	switch call.Tag {
	case "kind", "typeof":
		return true
	}
	return false
}

func demandForArrayOrSetExpr(elems []dag.VectorElem) demand.Demand {
	d := demand.None()
	for _, e := range elems {
		switch e := e.(type) {
		case *dag.Spread:
			d = demand.Union(d, demandForExpr(e.Expr))
		case *dag.VectorValue:
			d = demand.Union(d, demandForExpr(e.Expr))
		default:
			panic(e)
		}
	}
	return d
}

func demandForAssignments(assignments []dag.Assignment, downstream demand.Demand) demand.Demand {
	d := downstream
	for _, a := range assignments {
		if _, ok := a.LHS.(*dag.ThisExpr); ok {
			// Assignment clobbers a static field.
			d = demand.Delete(d, demandForExpr(a.LHS))
		} else {
			// Add anything needed by a dynamic field.
			d = demand.Union(d, demandForExpr(a.LHS))
		}
		d = demand.Union(d, demandForExpr(a.RHS))
	}
	return d
}

func demandForSortExprs(sortExprs []dag.SortExpr, downstream demand.Demand) demand.Demand {
	if len(sortExprs) == 0 {
		// Need all fields to guess sort key.
		return demand.All()
	}
	d := downstream
	for _, s := range sortExprs {
		d = demand.Union(d, demandForExpr(s.Key))
	}
	return d
}
