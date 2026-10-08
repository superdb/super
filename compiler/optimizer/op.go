package optimizer

import (
	"errors"
	"strings"

	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/field"
)

// analyzeSortKeys returns how an input order maps to an output order based
// on the semantics of the operator.  Note that an order can go from unknown
// to known (e.g., sort) or from known to unknown (e.g., conflicting parallel paths).
// Also, when op is an Aggregate operator, its input direction (where the
// order key is presumed to be the primary grouping key) is set based
// on the in sort key.  This is clumsy and needs to change.
// See issue #2658.
func (o *Optimizer) analyzeSortKeys(op dag.Op, in order.SortKeys) (order.SortKeys, error) {
	switch op := op.(type) {
	case *dag.PoolScan:
		// Ignore in and just return the sort order of the pool.
		pool, err := o.lookupPool(op.ID)
		if err != nil {
			return nil, err
		}
		return pool.SortKeys, nil
	case *dag.SortOp:
		return sortKeysOfSortExprs(op.Exprs), nil
	case *dag.TopOp:
		return sortKeysOfSortExprs(op.Exprs), nil
	}
	// We should handle secondary keys at some point.
	// See issue #2657.
	if in.IsNil() {
		return nil, nil
	}
	key := in.Primary()
	switch op := op.(type) {
	case *dag.ListerScan:
		// This shouldn't happen.
		return nil, errors.New("internal error: dag.Lister encountered in anaylzeSortKeys")
	case *dag.FilterOp, *dag.HeadOp, *dag.InferOp, *dag.PassOp, *dag.UniqOp, *dag.TailOp, *dag.FuseOp, *dag.OutputOp:
		return in, nil
	case *dag.CutOp:
		return analyzeCuts(op.Args, in), nil
	case *dag.DropOp:
		for _, f := range op.Args {
			if fieldOf(f).Equal(key.Path) {
				return nil, nil
			}
		}
		return in, nil
	case *dag.RenameOp:
		out := in
		for _, assignment := range op.Args {
			if fieldOf(assignment.RHS).Equal(key.Path) {
				lhs := fieldOf(assignment.LHS)
				out = order.SortKeys{order.NewSortKey(key.Order, lhs)}
			}
		}
		return out, nil
	case *dag.AggregateOp:
		if isKeyOfAggregate(op, in) {
			return in, nil
		}
		return nil, nil
	case *dag.PutOp:
		for _, assignment := range op.Args {
			if fieldOf(assignment.LHS).Equal(key.Path) {
				return nil, nil
			}
		}
		return in, nil
	default:
		return nil, nil
	}
}

func sortKeysOfSortExprs(exprs []dag.SortExpr) order.SortKeys {
	// XXX Only single sort keys.  See issue #2657.
	if len(exprs) != 1 {
		return nil
	}
	key, ok := sortKeyOfExpr(exprs[0].Key, exprs[0].Order)
	if !ok {
		return nil
	}
	return order.SortKeys{key}
}

func sortKeyOfExpr(e dag.Expr, o order.Which) (order.SortKey, bool) {
	key := fieldOf(e)
	if key == nil {
		return order.SortKey{}, false
	}
	return order.NewSortKey(o, key), true
}

// isKeyOfAggregate returns true iff any of a's grouping keys is the
// same as the given primary-key sort order or an order-preserving function
// thereof.
func isKeyOfAggregate(a *dag.AggregateOp, in order.SortKeys) bool {
	if in.IsNil() {
		return false
	}
	key := in[0].Path
	for _, outputKeyExpr := range a.Keys {
		groupingKey := fieldOf(outputKeyExpr.LHS)
		if groupingKey.Equal(key) {
			rhsExpr := outputKeyExpr.RHS
			rhs := fieldOf(rhsExpr)
			if rhs.Equal(key) || orderPreservingCall(rhsExpr, groupingKey) {
				return true
			}
		}
	}
	return false
}

func orderPreservingCall(e dag.Expr, key field.Path) bool {
	if call, ok := e.(*dag.CallExpr); ok {
		switch call.Tag {
		// There are probably other functions we could cover.
		// It would be good if the function declaration included
		// the info we need here.  See issue #2660.
		case "bucket", "ceil", "floor", "round":
			if len(call.Args) >= 1 && fieldOf(call.Args[0]).Equal(key) {
				return true
			}
		}
	}
	return false
}

func analyzeCuts(assignments []dag.Assignment, sortKeys order.SortKeys) order.SortKeys {
	if sortKeys.IsNil() {
		return nil
	}
	key := sortKeys[0].Path
	// This loop implements a very simple data flow analysis where we
	// track the known order through the scoreboard.  If on exit, there
	// is more than one field of known order, the current optimization
	// framework cannot handle this so we return unknown (order.Nil)
	// as a conservative stance to prevent any problematic optimizations.
	// If there is precisely one field of known order, then that is the
	// sort key we return.  In a future version of the optimizer, we will
	// generalize this scoreboard concept across the flowgraph for a
	// comprehensive approach to dataflow analysis.  See issue #2756.
	scoreboard := make(map[string]field.Path)
	scoreboard[fieldKey(key)] = key
	for _, a := range assignments {
		lhs := fieldOf(a.LHS)
		rhs := fieldOf(a.RHS)
		if lhs == nil {
			// If we cannot statically determine the data flow,
			// we give up and return unknown.  This is overly
			// conservative in general and will miss optimization
			// opportunities, e.g., we could do dependency
			// analysis of a complex RHS expression.
			return nil
		}
		lhsKey := fieldKey(lhs)
		if rhs == nil {
			// If the RHS depends on a well-defined set of fields
			// (none of which are unambiguous like this.foo[this.bar]),
			// and if all of such dependencies do not have an order
			// to preserve, then we can continue along by clearing
			// the LHS from the scoreboard knowing that is being set
			// to something that does not have a defined order.
			dependencies, ok := FieldsOf(a.RHS)
			if !ok {
				return nil
			}
			for _, d := range dependencies {
				key := fieldKey(d)
				if _, ok := scoreboard[key]; ok {
					// There's a dependency on an ordered
					// field but we're not sophisticated
					// enough here to know if this preserves
					// its order...
					return nil
				}
			}
			// There are no RHS dependencies on an ordered input.
			// Clobber the LHS if present from the scoreboard and continue.
			delete(scoreboard, lhsKey)
			continue
		}
		rhsKey := fieldKey(rhs)
		if _, ok := scoreboard[rhsKey]; ok {
			scoreboard[lhsKey] = lhs
			continue
		}
		// LHS is in the scoreboard and the RHS isn't, so
		// we know for sure there is no ordering guarantee
		// on the LHS field.  So clobber it and continue.
		delete(scoreboard, lhsKey)
	}
	if len(scoreboard) != 1 {
		return nil
	}
	for _, f := range scoreboard {
		return order.SortKeys{order.NewSortKey(sortKeys[0].Order, f)}
	}
	panic("unreachable")
}

func fieldKey(f field.Path) string {
	return strings.Join(f, "\x00")
}

func fieldOf(e dag.Expr) field.Path {
	if this, ok := e.(*dag.ThisExpr); ok {
		return this.Chain.Path()
	}
	return nil
}

func FieldsOf(e dag.Expr) (field.List, bool) {
	if e == nil {
		return nil, false
	}
	switch e := e.(type) {
	case *dag.SearchExpr, *dag.PrimitiveExpr:
		return nil, true
	case *dag.ThisExpr:
		return field.List{e.Chain.Path()}, true
	case *dag.UnaryExpr:
		return FieldsOf(e.Operand)
	case *dag.BinaryExpr:
		lhs, ok := FieldsOf(e.LHS)
		if !ok {
			return nil, false
		}
		rhs, ok := FieldsOf(e.RHS)
		if !ok {
			return nil, false
		}
		return append(lhs, rhs...), true
	case *dag.CondExpr:
		// finish with issue #2756
		return nil, false
	case *dag.CallExpr:
		// finish with issue #2756
		return nil, false
	case *dag.RegexpMatchExpr:
		return FieldsOf(e.Expr)
	case *dag.RecordExpr:
		// finish with issue #2756
		return nil, false
	case *dag.ArrayExpr:
		// finish with issue #2756
		return nil, false
	case *dag.SetExpr:
		// finish with issue #2756
		return nil, false
	case *dag.MapExpr:
		// finish with issue #2756
		return nil, false
	default:
		return nil, false
	}
}
