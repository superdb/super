package expr

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

// TypeAssert is a type assertion, e.(T), whose value is the value of e when
// e has type T and none otherwise.  A value whose type is T (or a member of
// T if T is a union type), including a union, option, or named value, is the
// value.  Otherwise, a union value is asserted on its member, an option value
// on its some value (and an option's none is none), and a fusion value on its
// subtype (see typeSwitch).
// A type assertion does not convert values, so, e.g., "1".(int64) is none.
// An error or none is passed through unless it has type T.
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

// assert applies the type assertion to vec (see typeSwitch).  The value is
// the value at the outermost level whose type has the asserted type.
func (t *TypeAssert) assert(vec vector.Any, target super.Type) vector.Any {
	return typeSwitch(vec, nil, func(levels []vector.Any, _ vector.Any) vector.Any {
		for _, level := range levels {
			if hasType(level.Type(), target) {
				return level
			}
		}
		leaf := levels[len(levels)-1]
		if passThrough(leaf.Type()) {
			return leaf
		}
		return vector.NewNone(leaf.Len())
	})
}

// typeSwitch splits vec into the subsets of its values that have the same
// types, as for a type assertion or a match expression, and returns the
// results of fn for each subset stitched together.  The types of a value are
// the types of its levels: the value itself and, for a union value, the
// levels of its member or, for an option value, the levels of its some value
// (an option's none has only the option level).  fn is called with the
// levels of a subset, outermost first, and with aligned (which may be nil)
// picked to the subset's slots.
func typeSwitch(vec, aligned vector.Any, fn func(levels []vector.Any, aligned vector.Any) vector.Any) vector.Any {
	return typeSwitchLevels(nil, vec, aligned, fn)
}

func typeSwitchLevels(outer []vector.Any, vec, aligned vector.Any, fn func([]vector.Any, vector.Any) vector.Any) vector.Any {
	return applyAligned(vector.ApplyNone, vec, aligned, outer, func(vec, aligned vector.Any, outer []vector.Any) vector.Any {
		levels := append(slices.Clone(outer), vec)
		switch v := vector.Under(vec).(type) {
		case *vector.Union:
			return typeSwitchLevels(levels, v.Dynamic(), aligned, fn)
		case *vector.Option:
			return applyAligned(vector.ApplyRipOptions, v, aligned, levels, func(vec, aligned vector.Any, levels []vector.Any) vector.Any {
				// This is a pure some or pure none since options are
				// ripped.  levels ends with the option value.
				o := vector.Under(vec).(*vector.Option)
				if _, ok := o.Any.(*vector.None); ok {
					return fn(levels, aligned)
				}
				return typeSwitchLevels(levels, o.Any, aligned, fn)
			})
		}
		return fn(levels, aligned)
	})
}

// applyAligned calls vector.Apply on vec with aligned (if not nil) and others
// picked to the slots of each subset of vec that Apply rips.
func applyAligned(opt vector.ApplyOpt, vec, aligned vector.Any, others []vector.Any, fn func(vec, aligned vector.Any, others []vector.Any) vector.Any) vector.Any {
	args := []vector.Any{vec}
	if aligned != nil {
		args = append(args, &vector.NoRip{Any: aligned})
	}
	for _, o := range others {
		args = append(args, &vector.NoRip{Any: o})
	}
	return vector.Apply(opt, func(vecs ...vector.Any) vector.Any {
		vec, rest := vecs[0], vecs[1:]
		var aligned vector.Any
		if len(rest) > len(others) {
			aligned, rest = rest[0], rest[1:]
		}
		return fn(vec, aligned, rest)
	}, args...)
}

// passThrough returns true for the types of values passed through by a type
// assertion whose type isn't the asserted type: errors and none.
func passThrough(typ super.Type) bool {
	switch typ := super.TypeUnder(typ).(type) {
	case *super.TypeError:
		return true
	default:
		return typ == super.TypeNone
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
