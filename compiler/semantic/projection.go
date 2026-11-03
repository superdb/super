package semantic

import (
	"fmt"
	"slices"

	"github.com/superdb/super/compiler/semantic/sem"
	"github.com/superdb/super/pkg/field"
)

func replaceGroupings(t *translator, in sem.Expr, groupings []exprloc) (sem.Expr, bool) {
	ok := true
	out := exprWalk(in, func(e sem.Expr) (sem.Expr, bool) {
		if i := exprMatch(e, groupings); i >= 0 {
			return sem.NewThis(e, field.NewChain("g", groupTmp(i))), true
		}
		switch e := e.(type) {
		case *sem.ThisExpr:
			if len(e.Chain) >= 1 {
				s := e.Chain[0].ID
				switch s {
				case "in":
					ok = false
					t.error(e, fmt.Errorf("column %q must appear in GROUP BY clause", e.Chain[len(e.Chain)-1].ID))
					return e, true
				case "out", "g":
				default:
					panic(s)
				}
			}
			return e, false
		case *sem.AggFunc:
			// This should't happen as all AggFuncs should have been
			// turned into AggRefs before this is called.
			panic(e)
		case *sem.AggRef:
			return sem.NewThis(e.Node, field.NewChain("g", aggTmp(e.Index))), false
		}
		return e, false
	})
	return out, ok
}

func exprMatch(target sem.Expr, exprs []exprloc) int {
	return slices.IndexFunc(exprs, func(e exprloc) bool {
		return eqExpr(target, e.expr)
	})
}

type exprVisitor func(e sem.Expr) (sem.Expr, bool)

func exprWalk(e sem.Expr, visit exprVisitor) sem.Expr {
	var stop bool
	if e, stop = visit(e); stop {
		return e
	}
	switch e := e.(type) {
	case nil:
	case *sem.AggFunc:
		e.Expr = exprWalk(e, visit)
		e.Filter = exprWalk(e, visit)
	case *sem.ArrayExpr:
		e.Elems = exprWalkArrayElems(e.Elems, visit)
	case *sem.BadExpr:
	case *sem.BinaryExpr:
		e.LHS = exprWalk(e.LHS, visit)
		e.RHS = exprWalk(e.RHS, visit)
	case *sem.CallExpr:
		var out []sem.Expr
		for _, arg := range e.Args {
			out = append(out, exprWalk(arg, visit))
		}
		e.Args = out
	case *sem.CondExpr:
		e.Cond = exprWalk(e.Cond, visit)
		e.Then = exprWalk(e.Then, visit)
		e.Else = exprWalk(e.Else, visit)
	case *sem.DotExpr:
		e.LHS = exprWalk(e.LHS, visit)
	case *sem.IndexExpr:
		e.Expr = exprWalk(e.Expr, visit)
		e.Index = exprWalk(e.Index, visit)
	case *sem.IsNullExpr:
		e.Expr = exprWalk(e.Expr, visit)
	case *sem.TypeAssertExpr:
		e.Expr = exprWalk(e.Expr, visit)
		e.Type = exprWalk(e.Type, visit)
	case *sem.MatchExpr:
		e.Expr = exprWalk(e.Expr, visit)
		for k := range e.Cases {
			e.Cases[k].Type = exprWalk(e.Cases[k].Type, visit)
			e.Cases[k].Expr = exprWalk(e.Cases[k].Expr, visit)
		}
		e.Else = exprWalk(e.Else, visit)
	case *sem.MapExpr:
		for _, ent := range e.Entries {
			ent.Key = exprWalk(ent.Key, visit)
			ent.Value = exprWalk(ent.Value, visit)
		}
	case *sem.PrimitiveExpr:
	case *sem.RecordExpr:
		var out []sem.RecordElem
		for _, elem := range e.Elems {
			switch elem := elem.(type) {
			case *sem.FieldElem:
				e := exprWalk(elem.Value, visit)
				out = append(out, &sem.FieldElem{Node: elem, Name: elem.Name, Value: e})
			case *sem.SpreadElem:
				e := exprWalk(elem.Expr, visit)
				out = append(out, &sem.SpreadElem{Node: elem, Expr: e})
			default:
				panic(elem)
			}
		}
		e.Elems = out
	case *sem.RegexpMatchExpr:
		e.Expr = exprWalk(e.Expr, visit)
	case *sem.RegexpSearchExpr:
		e.Expr = exprWalk(e.Expr, visit)
	case *sem.SearchTermExpr:
		e.Expr = exprWalk(e.Expr, visit)
	case *sem.SetExpr:
		e.Elems = exprWalkArrayElems(e.Elems, visit)
	case *sem.SliceExpr:
		e.Expr = exprWalk(e.Expr, visit)
		e.From = exprWalk(e.From, visit)
		e.To = exprWalk(e.To, visit)
	case *sem.SubqueryExpr: // XXX This might need to be traversed?
	case *sem.ThisExpr:
	case *sem.TypeExpr:
	case *sem.UnaryExpr:
		e.Operand = exprWalk(e.Operand, visit)
	default:
		panic(e)
	}
	return e
}

func exprWalkArrayElems(elems []sem.ArrayElem, visit exprVisitor) []sem.ArrayElem {
	var out []sem.ArrayElem
	for _, elem := range elems {
		switch elem := elem.(type) {
		case *sem.SpreadElem:
			e := exprWalk(elem.Expr, visit)
			out = append(out, &sem.SpreadElem{Node: elem.Node, Expr: e})
		case *sem.ExprElem:
			e := exprWalk(elem.Expr, visit)
			out = append(out, &sem.ExprElem{Node: elem.Node, Expr: e})
		default:
			panic(elem)
		}
	}
	return out
}
