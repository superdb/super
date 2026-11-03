package expr

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

// Match is a match expression, whose value is the value of the first case
// whose type is the type of the matched value (as for TypeAssert) or else
// the value of its else expression or none if it has none.  An error or
// missing value that no case matches is passed through.
type Match struct {
	sctx     *super.Context
	expr     Evaluator
	types    []Evaluator
	cases    []Evaluator
	elseExpr Evaluator
	defuse   *SuperDefuse
}

func NewMatch(sctx *super.Context, e Evaluator, types, cases []Evaluator, elseExpr Evaluator) *Match {
	return &Match{
		sctx:     sctx,
		expr:     e,
		types:    types,
		cases:    cases,
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
	return m.dispatch(m.defuse.Eval(m.expr.Eval(this)), this, targets)
}

func (m *Match) dispatch(vec, this vector.Any, targets []super.Type) vector.Any {
	return vector.Apply(vector.ApplyRipUnions|vector.ApplyRipOptions, func(vecs ...vector.Any) vector.Any {
		return m.eval(vecs[0], vecs[1], targets)
	}, vec, &vector.NoRip{Any: this})
}

func (m *Match) eval(vec, this vector.Any, targets []super.Type) vector.Any {
	if o, ok := vector.Under(vec).(*vector.Option); ok {
		// This is a pure some or pure none since options are ripped.
		if _, ok := o.Any.(*vector.None); !ok {
			return m.dispatch(o.Any, this, targets)
		}
	} else {
		typ := vec.Type()
		for k, target := range targets {
			if hasType(typ, target) {
				return m.cases[k].Eval(this)
			}
		}
		switch super.TypeUnder(typ).(type) {
		case *super.TypeError, *super.TypeOfMissing:
			return vec
		}
	}
	if m.elseExpr != nil {
		return m.elseExpr.Eval(this)
	}
	return vector.NewNone(vec.Len())
}
