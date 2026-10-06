package rungen

import (
	"errors"
	"fmt"
	"slices"

	"github.com/brimdata/super"
	"github.com/brimdata/super/compiler/dag"
	samexpr "github.com/brimdata/super/runtime/sam/expr"
	"github.com/brimdata/super/runtime/vam/expr"
	"github.com/brimdata/super/runtime/vam/expr/function"
	"github.com/brimdata/super/runtime/vam/op"
	"github.com/brimdata/super/sup"
	"github.com/brimdata/super/vector/vio"
	"golang.org/x/text/unicode/norm"
)

func (b *Builder) compileVamExpr(e dag.Expr) (expr.Evaluator, error) {
	if e == nil {
		return nil, errors.New("null expression not allowed")
	}
	switch e := e.(type) {
	case *dag.ArrayExpr:
		return b.compileVamArrayExpr(e)
	case *dag.BinaryExpr:
		return b.compileVamBinary(e)
	case *dag.CondExpr:
		return b.compileVamConditional(*e)
	case *dag.CallExpr:
		return b.compileVamCall(e)
	case *dag.DotExpr:
		return b.compileVamDotExpr(e)
	case *dag.IndexExpr:
		return b.compileVamIndexExpr(e)
	case *dag.IsNullExpr:
		return b.compileVamIsNullExpr(e)
	case *dag.MapCallExpr:
		return b.compileVamMapCallExpr(e)
	case *dag.MapExpr:
		return b.compileVamMapExpr(e)
	case *dag.PrimitiveExpr:
		val, err := sup.ParseValue(b.sctx(), e.Value)
		if err != nil {
			return nil, err
		}
		return expr.NewLiteral(b.sctx(), val), nil
	case *dag.RecordExpr:
		return b.compileVamRecordExpr(e)
	case *dag.RegexpMatchExpr:
		return b.compileVamRegexpMatch(e)
	case *dag.RegexpSearchExpr:
		return b.compileVamRegexpSearch(e)
	case *dag.SearchExpr:
		return b.compileVamSearch(e)
	case *dag.SetExpr:
		return b.compileVamSetExpr(e)
	case *dag.SliceExpr:
		return b.compileVamSliceExpr(e)
	case *dag.SubqueryExpr:
		return b.compileVamSubquery(e)
	case *dag.ThisExpr:
		return expr.NewDottedExpr(b.sctx(), e.Chain), nil
	case *dag.TypeExpr:
		typ, err := b.lookupType(e.ID)
		if err != nil {
			return nil, err
		}
		return expr.NewLiteral(b.rctx.Sctx, b.rctx.Sctx.LookupTypeValue(typ)), nil
	case *dag.UnaryExpr:
		return b.compileVamUnary(*e)
	default:
		return nil, fmt.Errorf("vector expression type %T: not supported", e)
	}
}

func (b *Builder) compileVamExprWithEmpty(e dag.Expr) (expr.Evaluator, error) {
	if e == nil {
		return nil, nil
	}
	return b.compileVamExpr(e)
}

func (b *Builder) compileVamBinary(e *dag.BinaryExpr) (expr.Evaluator, error) {
	//XXX TBD
	//if e.Op == "in" {
	// Do a faster comparison if the LHS is a compile-time constant expression.
	//	if in, err := b.compileConstIn(e); in != nil && err == nil {
	//		return in, err
	//	}
	//}
	// XXX don't think we need this... callee can check for const
	//if e, err := b.compileVamConstCompare(e); e != nil && err == nil {
	//	return e, nil
	//}
	lhs, err := b.compileVamExpr(e.LHS)
	if err != nil {
		return nil, err
	}
	rhs, err := b.compileVamExpr(e.RHS)
	if err != nil {
		return nil, err
	}
	switch op := e.Op; op {
	case "and":
		return expr.NewLogicalAnd(b.sctx(), lhs, rhs), nil
	case "or":
		return expr.NewLogicalOr(b.sctx(), lhs, rhs), nil
	case "in":
		return expr.NewIn(b.sctx(), lhs, rhs), nil
	case "==", "!=", "<", "<=", ">", ">=":
		return expr.NewCompare(b.sctx(), op, lhs, rhs), nil
	case "+", "-", "*", "/", "%":
		return expr.NewArith(b.sctx(), op, lhs, rhs), nil
	case "??":
		return expr.NewNoneish(lhs, rhs), nil
	default:
		return nil, fmt.Errorf("invalid binary operator %s", op)
	}
}

func (b *Builder) compileVamConditional(node dag.CondExpr) (expr.Evaluator, error) {
	predicate, err := b.compileVamExpr(node.Cond)
	if err != nil {
		return nil, err
	}
	thenExpr, err := b.compileVamExpr(node.Then)
	if err != nil {
		return nil, err
	}
	elseExpr, err := b.compileVamExpr(node.Else)
	if err != nil {
		return nil, err
	}
	return expr.NewConditional(b.sctx(), predicate, thenExpr, elseExpr), nil
}

func (b *Builder) compileVamUnary(unary dag.UnaryExpr) (expr.Evaluator, error) {
	e, err := b.compileVamExpr(unary.Operand)
	if err != nil {
		return nil, err
	}
	switch unary.Op {
	case "-":
		return expr.NewUnaryMinus(b.sctx(), e), nil
	case "!":
		return expr.NewLogicalNot(b.sctx(), e), nil
	default:
		return nil, fmt.Errorf("unknown unary operator %s", unary.Op)
	}
}

func (b *Builder) compileVamDotExpr(dot *dag.DotExpr) (expr.Evaluator, error) {
	record, err := b.compileVamExpr(dot.LHS)
	if err != nil {
		return nil, err
	}
	return expr.NewDotExpr(b.sctx(), record, dot.RHS, dot.Noneish, dot.Nullish), nil
}

func (b *Builder) compileVamIndexExpr(idx *dag.IndexExpr) (expr.Evaluator, error) {
	e, err := b.compileVamExpr(idx.Expr)
	if err != nil {
		return nil, err
	}
	index, err := b.compileVamExpr(idx.Index)
	if err != nil {
		return nil, err
	}
	return expr.NewIndexExpr(b.sctx(), e, index, idx.Base1), nil
}

func (b *Builder) compileVamIsNullExpr(idx *dag.IsNullExpr) (expr.Evaluator, error) {
	e, err := b.compileVamExpr(idx.Expr)
	if err != nil {
		return nil, err
	}
	return expr.NewIsNull(e), nil
}

func (b *Builder) compileVamExprs(in []dag.Expr) ([]expr.Evaluator, error) {
	var exprs []expr.Evaluator
	for _, e := range in {
		ev, err := b.compileVamExpr(e)
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, ev)
	}
	return exprs, nil
}

func (b *Builder) compileVamCall(call *dag.CallExpr) (expr.Evaluator, error) {
	var fn expr.Function
	if f, ok := b.funcs[call.Tag]; ok {
		var err error
		if fn, err = b.compileVamUDFCall(call.Tag, f); err != nil {
			return nil, err
		}
	} else {
		var err error
		fn, err = function.New(b.sctx(), call.Tag, len(call.Args))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", call.Tag, err)
		}
	}
	exprs, err := b.compileVamExprs(call.Args)
	if err != nil {
		return nil, err
	}
	if call.Tag == "cast" {
		if literal, ok := exprs[1].(*expr.Literal); ok {
			if cast, err := expr.NewLiteralCast(b.sctx(), exprs[0], literal); err == nil {
				return cast, nil
			}
		}
	}
	// Any call that expects zero arguments must take one argument
	// consisting of a vector that can represent the length of the argument
	// vector so we just pass in "this".
	if _, ok := fn.(function.NeedsInput); ok || len(exprs) == 0 {
		exprs = slices.Insert(exprs, 0, expr.NewDottedExpr(b.sctx(), nil))
	}
	return expr.NewCall(b.rctx.Sctx, fn, exprs), nil
}

func (b *Builder) compileVamUDFCall(tag string, f *dag.FuncDef) (expr.Function, error) {
	if fn, ok := b.compiledVamUDFs[tag]; ok {
		return fn, nil
	}
	fn := expr.NewUDF(b.sctx(), b.funcs[tag].Name, f.Params)
	// We store compiled UDF calls here so as to avoid stack overflows on
	// recursive calls.
	b.compiledVamUDFs[tag] = fn
	var err error
	if fn.Body, err = b.compileVamExpr(f.Expr); err != nil {
		return nil, err
	}
	delete(b.compiledVamUDFs, tag)
	return fn, nil
}

func (b *Builder) compileVamMapCallExpr(m *dag.MapCallExpr) (expr.Evaluator, error) {
	e, err := b.compileVamExpr(m.Expr)
	if err != nil {
		return nil, err
	}
	lambda, err := b.compileVamExpr(m.Lambda)
	if err != nil {
		return nil, err
	}
	return expr.NewMapCall(b.sctx(), e, lambda), nil
}

func (b *Builder) compileVamMapExpr(m *dag.MapExpr) (expr.Evaluator, error) {
	var entries []expr.Entry
	for _, entry := range m.Entries {
		key, err := b.compileVamExpr(entry.Key)
		if err != nil {
			return nil, err
		}
		val, err := b.compileVamExpr(entry.Value)
		if err != nil {
			return nil, err
		}
		entries = append(entries, expr.Entry{Key: key, Val: val})
	}
	return expr.NewMapExpr(b.sctx(), entries), nil
}

func (b *Builder) compileVamRecordExpr(e *dag.RecordExpr) (expr.Evaluator, error) {
	var elems []expr.RecordElem
	for _, elem := range e.Elems {
		switch elem := elem.(type) {
		case *dag.Field:
			e, err := b.compileVamExpr(elem.Value)
			if err != nil {
				return nil, err
			}
			elems = append(elems, &expr.FieldElem{
				Name: elem.Name,
				Opt:  elem.Opt,
				Expr: e,
			})
		case *dag.Spread:
			e, err := b.compileVamExpr(elem.Expr)
			if err != nil {
				return nil, err
			}
			elems = append(elems, &expr.SpreadElem{Expr: e})
		default:
			panic(elem)
		}
	}
	return expr.NewRecordExpr(b.sctx(), elems), nil
}

func (b *Builder) compileVamSubquery(query *dag.SubqueryExpr) (expr.Evaluator, error) {
	if !query.Correlated {
		exits, err := b.compileVamSeq(query.Body, nil)
		if err != nil {
			return nil, err
		}
		body := b.combineVam(exits)
		return op.NewCachedSubquery(b.sctx(), body), nil
	}
	var create func() *op.Subquery
	create = func() *op.Subquery {
		subquery := op.NewSubquery(b.rctx.Context, b.sctx(), func() *op.Subquery {
			b.mu.Lock()
			defer b.mu.Unlock()
			return create()
		})
		exits, err := b.compileVamSeq(query.Body, []vio.Puller{subquery})
		if err != nil {
			panic(err)
		}
		subquery.SetBody(b.combineVam(exits))
		return subquery
	}
	return create(), nil
}

func (b *Builder) compileVamRegexpMatch(match *dag.RegexpMatchExpr) (expr.Evaluator, error) {
	e, err := b.compileVamExpr(match.Expr)
	if err != nil {
		return nil, err
	}
	re, err := samexpr.CompileRegexp(match.Pattern)
	if err != nil {
		return nil, err
	}
	return expr.NewRegexpMatch(b.sctx(), re, e), nil
}

func (b *Builder) compileVamRegexpSearch(search *dag.RegexpSearchExpr) (expr.Evaluator, error) {
	e, err := b.compileVamExpr(search.Expr)
	if err != nil {
		return nil, err
	}
	re, err := samexpr.CompileRegexp(search.Pattern)
	if err != nil {
		return nil, err
	}
	return expr.NewSearchRegexp(re, e), nil
}

func (b *Builder) compileVamSearch(search *dag.SearchExpr) (expr.Evaluator, error) {
	val, err := sup.ParseValue(b.sctx(), search.Value)
	if err != nil {
		return nil, err
	}
	e, err := b.compileVamExpr(search.Expr)
	if err != nil {
		return nil, err
	}
	if super.TypeUnder(val.Type()) == super.TypeString {
		// Do a grep-style substring search instead of an
		// exact match on each value.
		term := norm.NFC.Bytes(val.Bytes())
		return expr.NewSearchString(string(term), e), nil
	}
	return expr.NewSearch(b.sctx(), search.Text, val, e), nil
}

func (b *Builder) compileVamSliceExpr(slice *dag.SliceExpr) (expr.Evaluator, error) {
	e, err := b.compileVamExpr(slice.Expr)
	if err != nil {
		return nil, err
	}
	from, err := b.compileVamExprWithEmpty(slice.From)
	if err != nil {
		return nil, err
	}
	to, err := b.compileVamExprWithEmpty(slice.To)
	if err != nil {
		return nil, err
	}
	return expr.NewSliceExpr(b.sctx(), e, from, to, slice.Base1), nil
}

func (b *Builder) compileVamArrayExpr(e *dag.ArrayExpr) (expr.Evaluator, error) {
	elems, err := b.compileVamListElems(e.Elems)
	if err != nil {
		return nil, err
	}
	return expr.NewArrayExpr(b.sctx(), elems), nil
}

func (b *Builder) compileVamSetExpr(e *dag.SetExpr) (expr.Evaluator, error) {
	elems, err := b.compileVamListElems(e.Elems)
	if err != nil {
		return nil, err
	}
	return expr.NewSetExpr(b.sctx(), elems), nil
}

func (b *Builder) compileVamListElems(elems []dag.VectorElem) ([]expr.ListElem, error) {
	var out []expr.ListElem
	for _, elem := range elems {
		switch elem := elem.(type) {
		case *dag.Spread:
			e, err := b.compileVamExpr(elem.Expr)
			if err != nil {
				return nil, err
			}
			out = append(out, expr.ListElem{Spread: e})
		case *dag.VectorValue:
			e, err := b.compileVamExpr(elem.Expr)
			if err != nil {
				return nil, err
			}
			out = append(out, expr.ListElem{Value: e})
		default:
			panic(elem)
		}
	}
	return out, nil
}
