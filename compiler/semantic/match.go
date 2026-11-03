package semantic

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/compiler/semantic/sem"
	"github.com/superdb/super/pkg/field"
)

// matchExpr translates a match expression.  When the matched expression is
// a path (e.g., x or this.a.b), the value at that path is narrowed in each
// case to the case's type, i.e., the case is type checked with the path
// having that type and each reference to the path in the case (or to a path
// beneath it) is replaced with a type assertion of the path to that type,
// which unwraps a union, option, or fusion value.
func (t *translator) matchExpr(e *ast.MatchExpr, inType super.Type) (sem.Expr, super.Type) {
	expr, exprType := t.expr(e.Expr, inType)
	this, isPath := expr.(*sem.ThisExpr)
	var path []string
	if isPath {
		for _, elem := range this.Chain {
			path = append(path, elem.ID)
		}
	}
	var cases []sem.MatchCase
	var targets, types []super.Type
	for _, c := range e.Cases {
		typExpr, _ := t.semType(c.Type)
		target := t.checker.typeOfTypeExpr(typExpr)
		armIn := inType
		if isPath && target != nil {
			armIn = t.checker.narrowPath(inType, path, target)
		}
		arm, armType := t.expr(c.Expr, armIn)
		if isPath {
			arm = narrowRefs(arm, this.Chain, typExpr)
		}
		cases = append(cases, sem.MatchCase{Type: typExpr, Expr: arm})
		targets = append(targets, target)
		types = append(types, armType)
	}
	var elseExpr sem.Expr
	if e.Else != nil {
		var elseType super.Type
		elseExpr, elseType = t.expr(e.Else, inType)
		types = append(types, elseType)
	}
	out := &sem.MatchExpr{Node: e, Expr: expr, Cases: cases, Else: elseExpr}
	return out, t.checker.matchType(exprType, targets, types, elseExpr != nil)
}

// narrowRefs returns e with each reference to the path chain, or to a path
// beneath it, replaced with a narrowed type assertion of chain to the type
// of typExpr.
func narrowRefs(e sem.Expr, chain field.Chain, typExpr sem.Expr) sem.Expr {
	return exprWalk(e, func(e sem.Expr) (sem.Expr, bool) {
		switch e := e.(type) {
		case *sem.AggFunc:
			// Don't descend into aggregate functions.
			return e, true
		case *sem.ThisExpr:
			if len(e.Chain) < len(chain) || !slices.EqualFunc(e.Chain[:len(chain)], chain, func(a, b field.ChainElem) bool {
				return a.ID == b.ID
			}) {
				return e, true
			}
			var out sem.Expr = &sem.TypeAssertExpr{
				Node:     e.Node,
				Expr:     &sem.ThisExpr{Node: e.Node, Chain: slices.Clone(e.Chain[:len(chain)])},
				Type:     sem.CopyExpr(typExpr),
				Narrowed: true,
			}
			for _, elem := range e.Chain[len(chain):] {
				out = &sem.DotExpr{Node: e.Node, LHS: out, RHS: elem.ID, Noneish: elem.Noneish, Nullish: elem.Nullish}
			}
			return out, true
		}
		return e, false
	})
}
