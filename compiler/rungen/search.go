package rungen

import (
	"github.com/superdb/super"
	"github.com/superdb/super/compiler/dag"
	samexpr "github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/runtime/search"
	"github.com/superdb/super/sup"
	"golang.org/x/text/unicode/norm"
)

// CompileSearch tries to return a search.Expr for e such that the
// e's Eval method returns true for any string containing the
// encoding of any value matching e. (It may also return true for some byte
// slices that do not match.) CompileSearch returns a nil pointer and nil
// error if it cannot construct a useful filter.
func CompileSearch(sctx *super.Context, e dag.Expr) (search.Expr, error) {
	switch e := e.(type) {
	case *dag.BinaryExpr:
		literal, err := isFieldEqualOrIn(sctx, e)
		if err != nil {
			return nil, err
		}
		if literal != nil {
			return newSearchForLiteral(*literal)
		}
		if e.Op == "and" {
			left, err := CompileSearch(sctx, e.LHS)
			if err != nil {
				return nil, err
			}
			right, err := CompileSearch(sctx, e.RHS)
			if err != nil {
				return nil, err
			}
			if left == nil {
				return right, nil
			}
			if right == nil {
				return left, nil
			}
			return search.NewAndExpr(left, right), nil
		}
		if e.Op == "or" {
			left, err := CompileSearch(sctx, e.LHS)
			if err != nil {
				return nil, err
			}
			right, err := CompileSearch(sctx, e.RHS)
			if left == nil || right == nil || err != nil {
				return nil, err
			}
			return search.NewOrExpr(left, right), nil
		}
		return nil, nil
	case *dag.SearchExpr:
		literal, err := sup.ParseValue(sctx, e.Value)
		if err != nil {
			return nil, err
		}
		if super.TypeUnder(literal.Type()) != super.TypeString {
			return nil, nil
		}
		pattern := norm.NFC.Bytes(literal.Bytes())
		return search.NewExprForStringCase(string(pattern)), nil
	case *dag.RegexpSearchExpr:
		if _, ok := e.Expr.(*dag.ThisExpr); !ok {
			return nil, nil
		}
		literal, fold, ok := samexpr.ParseLiteral(e.Pattern)
		if !ok {
			return nil, nil
		}
		if !fold {
			return search.NewExprForString(literal), nil
		}
		expr := search.NewExprForStringCase(literal)
		if odd := samexpr.OddFoldVariants(literal); expr != nil && len(odd) > 0 {
			expr = search.NewOrExpr(expr, search.NewExprForAnyString(odd))
		}
		return expr, nil
	default:
		return nil, nil
	}
}

func isFieldEqualOrIn(sctx *super.Context, e *dag.BinaryExpr) (*super.Value, error) {
	// XXX This could work for complex literal but we need to walk the
	// expression and compute a super.Value or call eval.
	if _, ok := e.LHS.(*dag.ThisExpr); ok && e.Op == "==" {
		if literal, ok := e.RHS.(*dag.PrimitiveExpr); ok {
			val, err := sup.ParseValue(sctx, literal.Value)
			if err != nil {
				return nil, err
			}
			return &val, nil
		}
	} else if _, ok := e.RHS.(*dag.ThisExpr); ok && e.Op == "in" {
		if literal, ok := e.LHS.(*dag.PrimitiveExpr); ok {
			val, err := sup.ParseValue(sctx, literal.Value)
			if err != nil {
				return nil, err
			}
			if val.Type() == super.TypeNet {
				return nil, err
			}
			return &val, nil
		}
	}
	return nil, nil
}

func newSearchForLiteral(val super.Value) (search.Expr, error) {
	if id := val.Type().ID(); super.IsNumber(id) || id == super.IDNull {
		return nil, nil
	}
	return search.NewExprForString(string(val.Bytes())), nil
}
