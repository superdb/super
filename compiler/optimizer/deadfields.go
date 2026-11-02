package optimizer

import (
	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/compiler/optimizer/demand"
)

// removeDeadFields removes the fields of record expressions in values
// operators that are not needed downstream according to table so that their
// inputs are not demanded upstream.  It returns true if anything was removed, in
// which case table is stale and should be recomputed since removing fields
// can reduce the demand on operators further upstream.
func removeDeadFields(table demandTable) bool {
	var changed bool
	for op, downstream := range table {
		values, ok := op.(*dag.ValuesOp)
		if !ok {
			continue
		}
		for k, e := range values.Exprs {
			if out, ok := removeDeadFieldsFromExpr(e, downstream); ok {
				values.Exprs[k] = out
				changed = true
			}
		}
	}
	return changed
}

// removeDeadFieldsFromExpr returns e with the fields of its record
// expressions that are not needed by downstream removed and true if anything
// was removed.
func removeDeadFieldsFromExpr(e dag.Expr, downstream demand.Demand) (dag.Expr, bool) {
	rec, ok := e.(*dag.RecordExpr)
	if !ok || demand.IsAll(downstream) {
		return e, false
	}
	var changed bool
	var elems []dag.RecordElem
	for _, elem := range rec.Elems {
		if f, ok := elem.(*dag.Field); ok {
			d := demand.GetKey(downstream, f.Name)
			if demand.IsNone(d) {
				changed = true
				continue
			}
			if value, ok := removeDeadFieldsFromExpr(f.Value, d); ok {
				elem = &dag.Field{Kind: "Field", Name: f.Name, Value: value}
				changed = true
			}
		}
		elems = append(elems, elem)
	}
	if !changed {
		return e, false
	}
	return &dag.RecordExpr{Kind: "RecordExpr", Elems: elems}, true
}
