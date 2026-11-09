package semantic

import (
	"slices"

	"github.com/superdb/super/compiler/semantic/sem"
)

// mayBeEmpty returns false if the body of subquery e always produces a value,
// e.g., (values a) or (sum(x)), and true if it might not, in which case the
// value of a pipe subquery is none.  It's conservative: a body that's not
// known to produce a value may be empty.
func mayBeEmpty(e *sem.SubqueryExpr) bool {
	// A correlated subquery runs on the value of its outer expression.
	nonEmpty := e.Correlated
	for _, op := range e.Body {
		nonEmpty = producesValue(op, nonEmpty)
	}
	return !nonEmpty
}

// producesValue returns true if op is known to produce a value when its input
// is nonEmpty.
func producesValue(op sem.Op, nonEmpty bool) bool {
	switch op := op.(type) {
	case *sem.NullScan:
		return true
	case *sem.AggregateOp:
		// An aggregate without keys produces one value even with no
		// input, and with keys, a value per group.
		return len(op.Keys) == 0 || nonEmpty
	case *sem.ValuesOp:
		// A value that can't be a plain none isn't dropped.
		return nonEmpty && slices.ContainsFunc(op.Exprs, notNone)
	case *sem.HeadOp:
		return nonEmpty && op.Count > 0
	case *sem.TailOp:
		return nonEmpty && op.Count > 0
	case *sem.CountOp, *sem.CutOp, *sem.DebugOp, *sem.DistinctOp, *sem.DropOp,
		*sem.FuseOp, *sem.PassOp, *sem.PutOp, *sem.RenameOp, *sem.SortOp,
		*sem.UniqOp:
		// These produce a value (possibly an error) for some input.
		return nonEmpty
	}
	return false
}

// notNone returns true if e is known not to be a plain none.
func notNone(e sem.Expr) bool {
	switch e := e.(type) {
	case *sem.PrimitiveExpr:
		return e.Value != "none"
	case *sem.ArrayExpr, *sem.MapExpr, *sem.RecordExpr, *sem.SetExpr:
		return true
	case *sem.DotExpr:
		return !e.Noneish && notNone(e.LHS)
	case *sem.IndexExpr:
		// An index out of range is an error.
		return notNone(e.Expr)
	case *sem.ThisExpr:
		// A field of a record and a value input to an operator are
		// never plain nones.
		for _, elem := range e.Chain {
			if elem.Noneish {
				return false
			}
		}
		return true
	}
	return false
}
