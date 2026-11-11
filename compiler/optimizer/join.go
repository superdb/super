package optimizer

import (
	"fmt"
	"reflect"

	"github.com/superdb/super/compiler/dag"
)

func liftFiltersIntoJoins(seq dag.Seq) dag.Seq {
	var filter *dag.FilterOp
	var i int
	for i = range len(seq) - 3 {
		_, isfork := seq[i].(*dag.ForkOp)
		_, isjoin := seq[i+1].(*dag.JoinOp)
		var isfilter bool
		filter, isfilter = plainFilter(seq[i+2])
		if isfork && isjoin && isfilter {
			break
		}
	}
	if filter == nil {
		return seq
	}
	in := splitPredicate(filter.Expr)
	var exprs []dag.Expr
	for _, e := range in {
		if b, ok := e.(*dag.BinaryExpr); ok && b.Op == "==" && liftFilterIntoJoin(seq[i:], b.LHS, b.RHS) {
			continue
		}
		exprs = append(exprs, e)
	}
	if len(exprs) == 0 {
		seq.Delete(i+2, i+3)
	} else if len(exprs) != len(in) {
		seq[i+2] = dag.NewFilterOp(buildConjunction(exprs))
	}
	return seq
}

func liftFilterIntoJoin(seq dag.Seq, lhs, rhs dag.Expr) bool {
	fork, isfork := seq[0].(*dag.ForkOp)
	join, isjoin := seq[1].(*dag.JoinOp)
	if !isfork || !isjoin {
		return false
	}
	if len(fork.Paths) != 2 {
		panic(fork)
	}
	lhsFirst, lok := firstThisPathComponent(lhs)
	rhsFirst, rok := firstThisPathComponent(rhs)
	if !lok || !rok {
		return false
	}
	if lhsFirst == rhsFirst {
		lhs, rhs = dag.CopyExpr(lhs), dag.CopyExpr(rhs)
		stripFirstThisPathComponent(lhs)
		stripFirstThisPathComponent(rhs)
		if lhsFirst == join.LeftAlias {
			return liftFilterIntoJoin(fork.Paths[0], lhs, rhs)
		}
		if lhsFirst == join.RightAlias {
			return liftFilterIntoJoin(fork.Paths[1], lhs, rhs)
		}
		return false
	}
	if lhsFirst != join.LeftAlias {
		lhsFirst, rhsFirst = rhsFirst, lhsFirst
		lhs, rhs = rhs, lhs
	}
	if lhsFirst != join.LeftAlias || rhsFirst != join.RightAlias {
		return false
	}
	cond := dag.NewBinaryExpr("==", lhs, rhs)
	if join.Cond != nil {
		cond = dag.NewBinaryExpr("and", join.Cond, cond)
	}
	join.Cond = cond
	return true
}

func replaceJoinWithHashJoin(seq dag.Seq) {
	walkT(reflect.ValueOf(seq), func(op dag.Op) dag.Op {
		j, ok := op.(*dag.JoinOp)
		if !ok {
			return op
		}
		var lefts, rights []dag.Expr
		for _, e := range splitPredicate(j.Cond) {
			left, right, ok := equiJoinKeyExprs(e, j.LeftAlias, j.RightAlias)
			if !ok {
				return op
			}
			lefts = append(lefts, left)
			rights = append(rights, right)
		}
		var left, right dag.Expr
		if len(lefts) == 1 {
			left, right = lefts[0], rights[0]
		} else {
			// XXX Perhaps we should merge record expressions?
			left = buildTuple(lefts)
			right = buildTuple(rights)
		}
		style := j.Style
		if style == "cross" {
			style = "inner"
		}
		return &dag.HashJoinOp{
			Kind:       "HashJoinOp",
			Style:      style,
			LeftAlias:  j.LeftAlias,
			RightAlias: j.RightAlias,
			LeftKey:    left,
			RightKey:   right,
			Catch:      j.Catch,
		}
	})
}

func buildTuple(exprs []dag.Expr) dag.Expr {
	var elems []dag.RecordElem
	for i, e := range exprs {
		elems = append(elems, &dag.Field{
			Kind:  "Field",
			Name:  fmt.Sprintf("c%d", i),
			Value: e,
		})
	}
	return &dag.RecordExpr{Kind: "RecordExpr", Elems: elems}
}

func equiJoinKeyExprs(e dag.Expr, leftAlias, rightAlias string) (left, right dag.Expr, ok bool) {
	b, ok := e.(*dag.BinaryExpr)
	if !ok || b.Op != "==" {
		return nil, nil, false
	}
	lhsFirst, ok := firstThisPathComponent(b.LHS)
	if !ok {
		return nil, nil, false
	}
	rhsFirst, ok := firstThisPathComponent(b.RHS)
	if !ok {
		return nil, nil, false
	}
	lhs, rhs := b.LHS, b.RHS
	if lhsFirst != leftAlias {
		lhsFirst, rhsFirst = rhsFirst, lhsFirst
		lhs, rhs = rhs, lhs
	}
	if lhsFirst != leftAlias || rhsFirst != rightAlias {
		return nil, nil, false
	}
	// This next step will mutate the input expression, do a copy here.
	lhs, rhs = dag.CopyExpr(lhs), dag.CopyExpr(rhs)
	stripFirstThisPathComponent(lhs)
	stripFirstThisPathComponent(rhs)
	return lhs, rhs, true
}

// firstThisPathComponent returns the first component common to every dag.This.Path
// in e and a Boolean indicating whether such a common first component exists.
func firstThisPathComponent(e dag.Expr) (prefix string, ok bool) {
	walkT(reflect.ValueOf(e), func(t dag.ThisExpr) dag.ThisExpr {
		if prefix == "" {
			prefix = t.Chain[0].ID
			ok = true
		} else if prefix != t.Chain[0].ID {
			ok = false
		}
		return t
	})
	return prefix, ok
}

// stripFirstThisPathComponent removes the first component of every dag.This.Path in e.
func stripFirstThisPathComponent(e dag.Expr) {
	walkT(reflect.ValueOf(e), func(t dag.ThisExpr) dag.ThisExpr {
		t.Chain = t.Chain[1:]
		return t
	})
}
