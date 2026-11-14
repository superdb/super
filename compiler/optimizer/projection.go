package optimizer

import (
	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/compiler/optimizer/demand"
	"github.com/superdb/super/pkg/field"
)

// setProjections pushes the demand in table down into the scans so they
// read only the fields that are needed.
func setProjections(table demandTable) {
	for op, downstream := range table {
		switch op := op.(type) {
		case *dag.FileScan:
			if mf := op.Pushdown.MetaFilter; mf != nil {
				mf.HasProjection = true
				mf.Projection = demand.Fields(demand.WithoutTypes(demandForExpr(mf.Expr)))
			}
			if df := op.Pushdown.DataFilter; df != nil {
				// The filters don't use projections with assertions.
				exprDemand := demand.WithoutAsserts(demandForExpr(df.Expr))
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
			op.Pushdown.AssertProjection = field.FlattenAsserts(demand.AssertProjection(downstream))
		case *dag.SeqScan:
			d := demand.Union(downstream, demandForExpr(op.Filter))
			d = demand.Union(d, demandForExpr(op.KeyPruner))
			op.Fields = demand.Fields(demand.WithoutTypes(d))
		}
	}
}

// demandForExpr returns the demand of expr on its input.
func demandForExpr(expr dag.Expr) demand.Demand {
	a := &demandAnalyzer{table: make(demandTable)}
	return a.demandForExpr(expr)
}
