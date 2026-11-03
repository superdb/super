package expr

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

// Match is a match expression, whose value is the value of the first case
// whose type the matched value has (as for TypeAssert, see typeSwitch) or else
// the value of its else expression or none if it has none.  An error or
// missing value that no case matches is passed through.
type Match struct {
	sctx     *super.Context
	expr     Evaluator
	types    []Evaluator
	cases    []Evaluator
	inputs   []Evaluator
	elseExpr Evaluator
	defuse   *SuperDefuse
}

// NewMatch returns a Match whose kth case has type types[k] and result
// cases[k], which is evaluated on inputs[k] applied to this (for a narrowed
// path) or, if inputs[k] is nil, on this.
func NewMatch(sctx *super.Context, e Evaluator, types, cases, inputs []Evaluator, elseExpr Evaluator) *Match {
	return &Match{
		sctx:     sctx,
		expr:     e,
		types:    types,
		cases:    cases,
		inputs:   inputs,
		elseExpr: elseExpr,
		defuse:   NewSuperDefuse(sctx),
	}
}

func (m *Match) Eval(this vector.Any) vector.Any {
	targets := make([]super.Type, 0, len(m.types))
	for _, typ := range m.types {
		// The case types are type expressions, so they're constant.
		targets = append(targets, vector.TypeValueValue(typ.Eval(this), 0))
	}
	vec := m.defuse.Eval(m.expr.Eval(this))
	// The values of vec are matched by the same rules as a type assertion,
	// so a value matches a case if any of its levels has the case's type.
	return typeSwitch(vec, this, func(levels []vector.Any, this vector.Any) vector.Any {
		for k, target := range targets {
			if slices.ContainsFunc(levels, func(level vector.Any) bool {
				return hasType(level.Type(), target)
			}) {
				in := this
				if m.inputs[k] != nil {
					in = m.inputs[k].Eval(this)
				}
				return m.cases[k].Eval(in)
			}
		}
		leaf := levels[len(levels)-1]
		switch super.TypeUnder(leaf.Type()).(type) {
		case *super.TypeError, *super.TypeOfMissing:
			return leaf
		}
		if m.elseExpr != nil {
			return m.elseExpr.Eval(this)
		}
		return vector.NewNone(leaf.Len())
	})
}
