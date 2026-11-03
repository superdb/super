package expr

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

// TypeAssert is a type assertion, e.(T), whose value is the value of e when
// e has type T and none otherwise.  A value whose type is T, including a
// union, option, or named value, is the value.  Otherwise, a union value is
// asserted on its member, an option value on its some value (and an option's
// none is none), and a fusion value on its subtype.  If T is a union type,
// a value whose type is a member of T is the value (with its member type).
// A type assertion does not convert values, so, e.g., "1".(int64) is none.
// An error or missing value is passed through unless it has type T.
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
	vec := t.defuse.Eval(t.expr.Eval(this))
	// The type is a type expression so it's constant.
	c, ok := t.typ.Eval(this).(*vector.Const)
	if !ok || c.Type() != super.TypeType {
		return vector.NewStringError(t.sctx, "type assertion: type value required", vec.Len())
	}
	return t.assert(vec, vector.TypeValueValue(c, 0))
}

// assert applies the type assertion to each type of value in vec.
func (t *TypeAssert) assert(vec vector.Any, target super.Type) vector.Any {
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		return t.eval(vecs[0], target)
	}, vec)
}

func (t *TypeAssert) eval(vec vector.Any, target super.Type) vector.Any {
	typ := vec.Type()
	if typ == target {
		// This includes a union, option, or named value of type target.
		return vec
	}
	switch v := vector.Under(vec).(type) {
	case *vector.Union:
		return t.assert(v.Dynamic(), target)
	case *vector.Option:
		return vector.Apply(vector.ApplyRipOptions, func(vecs ...vector.Any) vector.Any {
			// This is a pure some or pure none since options are ripped.
			o := vector.Under(vecs[0]).(*vector.Option)
			if _, ok := o.Any.(*vector.None); ok {
				return vector.NewNone(o.Len())
			}
			return t.assert(o.Any, target)
		}, v)
	}
	if passThrough(typ) || hasType(typ, target) {
		return vec
	}
	return vector.NewNone(vec.Len())
}

// passThrough returns true for the types of values passed through by a type
// assertion whose type isn't the asserted type: errors, missing, and none.
func passThrough(typ super.Type) bool {
	switch typ := super.TypeUnder(typ).(type) {
	case *super.TypeError:
		return true
	default:
		return typ == super.TypeMissing || typ == super.TypeNone
	}
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
