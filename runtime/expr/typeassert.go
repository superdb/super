package expr

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

// TypeAssert is a type assertion, e.(T), whose value is the value of e when
// e has type T and none otherwise.  A union value has the type of its
// member, an option value has the type of its some value (and an option's
// none is none), and a fusion value has its subtype.  If T is a union type,
// then a value whose type is a member of T has type T.  A type assertion
// does not convert values, so, e.g., "1".(int64) is none.  An error or
// missing value is passed through unless it has type T.
type TypeAssert struct {
	sctx   *super.Context
	expr   Evaluator
	typ    Evaluator
	defuse *SuperDefuse
}

func NewTypeAssert(sctx *super.Context, e, typ Evaluator) *TypeAssert {
	return &TypeAssert{sctx: sctx, expr: e, typ: typ, defuse: NewSuperDefuse(sctx)}
}

func (t *TypeAssert) Eval(this vector.Any) vector.Any {
	return t.assert(t.defuse.Eval(t.expr.Eval(this)), t.typ.Eval(this))
}

func (t *TypeAssert) assert(vec, types vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipUnions|vector.ApplyRipOptions, t.eval, vec, types)
}

func (t *TypeAssert) eval(vecs ...vector.Any) vector.Any {
	vec, types := vecs[0], vecs[1]
	if o, ok := vector.Under(vec).(*vector.Option); ok {
		// This is a pure some or pure none since options are ripped.
		if _, ok := o.Any.(*vector.None); ok {
			return vector.NewNone(o.Len())
		}
		return t.assert(o.Any, types)
	}
	typ := vec.Type()
	// Errors, missing, and none are passed through when they're not of
	// the asserted type.
	pass := super.TypeUnder(typ) == super.TypeMissing || super.TypeUnder(typ) == super.TypeNone
	if _, ok := super.TypeUnder(typ).(*super.TypeError); ok {
		pass = true
	}
	if c, ok := types.(*vector.Const); ok {
		if pass || hasType(typ, vector.TypeValueValue(c, 0)) {
			return vec
		}
		return vector.NewNone(vec.Len())
	}
	var index, nones []uint32
	for i := range vec.Len() {
		if pass || hasType(typ, vector.TypeValueValue(types, i)) {
			index = append(index, i)
		} else {
			nones = append(nones, i)
		}
	}
	switch {
	case len(nones) == 0:
		return vec
	case len(index) == 0:
		return vector.NewNone(vec.Len())
	}
	return vector.Combine(vector.Pick(vec, index), nones, vector.NewNone(uint32(len(nones))))
}

// hasType returns true if a value of type typ has the asserted type target,
// i.e., typ is target or a member of the union type target.
func hasType(typ, target super.Type) bool {
	if typ == target {
		return true
	}
	if u, ok := super.TypeUnder(target).(*super.TypeUnion); ok {
		return slices.Contains(u.Types, typ)
	}
	return false
}
