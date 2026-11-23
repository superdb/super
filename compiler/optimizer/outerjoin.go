package optimizer

import "github.com/superdb/super/compiler/dag"

// convertOuterJoins turns an outer join into an inner join when the filter
// that follows it can never be true for a value the join leaves unmatched.
// Such a filter removes every unmatched value anyway, so the result is the
// same, and the filter can then be pushed into the join's inputs or lifted
// into its condition like any filter after an inner join.
func convertOuterJoins(seq dag.Seq) dag.Seq {
	for i, op := range seq {
		fork, ok := op.(*dag.ForkOp)
		if !ok {
			continue
		}
		for k := range fork.Paths {
			fork.Paths[k] = convertOuterJoins(fork.Paths[k])
		}
		if i+2 >= len(seq) || len(fork.Paths) != 2 {
			continue
		}
		join, ok := seq[i+1].(*dag.JoinOp)
		if !ok {
			continue
		}
		filter, ok := seq[i+2].(*dag.FilterOp)
		if !ok {
			continue
		}
		switch join.Style {
		case "left":
			// An unmatched value of a left join has no right side.
			if rejectsMissing(filter.Expr, join.RightAlias) {
				join.Style = "inner"
			}
		case "right":
			if rejectsMissing(filter.Expr, join.LeftAlias) {
				join.Style = "inner"
			}
		}
	}
	return seq
}

// rejectsMissing reports whether predicate e is never true when field alias
// is missing, i.e., e has a conjunct comparing a path under alias.  When an
// operand of a comparison is null (as for a missing column in SQL) or an error
// (as for a missing field), the comparison is null or an error, which a filter
// treats as false.  A path read with ?. is none instead, and none is unequal
// to every other value, so only equality with a literal rejects it.
func rejectsMissing(e dag.Expr, alias string) bool {
	for _, conjunct := range splitPredicate(e) {
		b, ok := conjunct.(*dag.BinaryExpr)
		if !ok {
			continue
		}
		switch b.Op {
		case "==", "!=", "<", "<=", ">", ">=":
			if comparesMissing(b.Op, b.LHS, b.RHS, alias) || comparesMissing(b.Op, b.RHS, b.LHS, alias) {
				return true
			}
		}
	}
	return false
}

// comparesMissing reports whether comparison "path op other" is never true
// when field alias is missing, where path is a path under alias.
func comparesMissing(op string, path, other dag.Expr, alias string) bool {
	this, ok := path.(*dag.ThisExpr)
	if !ok || len(this.Chain) == 0 || this.Chain[0].ID != alias {
		return false
	}
	for _, elem := range this.Chain {
		if elem.Noneish {
			_, isLiteral := other.(*dag.PrimitiveExpr)
			return op == "==" && isLiteral
		}
	}
	return true
}
