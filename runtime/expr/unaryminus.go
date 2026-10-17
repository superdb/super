package expr

import (
	"math"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr/cast"
	"github.com/superdb/super/vector"
)

type unaryMinus struct {
	sctx *super.Context
	expr Evaluator
}

func NewUnaryMinus(sctx *super.Context, eval Evaluator) Evaluator {
	return &unaryMinus{sctx, eval}
}

func (u *unaryMinus) Eval(this vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipUnions|vector.ApplyRipFusions, u.eval, u.expr.Eval(this))
}

func (u *unaryMinus) eval(vecs ...vector.Any) vector.Any {
	if vec, ok := Catch(u.sctx, true, vecs, "unary '-'"); ok {
		return vec
	}
	vec := vector.Under(vecs[0])
	id := vec.Type().ID()
	if !super.IsNumber(id) {
		return vector.NewWrappedError(u.sctx, "type incompatible with unary '-' operator", vecs[0])
	}
	if super.IsUnsigned(id) {
		var typ super.Type
		switch id {
		case super.IDUint8:
			typ = super.TypeInt8
		case super.IDUint16:
			typ = super.TypeInt16
		case super.IDUint32:
			typ = super.TypeInt32
		default:
			typ = super.TypeInt64
		}
		return u.eval(cast.To(u.sctx, vec, typ))
	}
	out, ok := u.convert(vec)
	if !ok {
		// Overflow for int detected, go slow path.
		return u.slowPath(vec)
	}
	return out
}

func (u *unaryMinus) convert(vec vector.Any) (vector.Any, bool) {
	switch vec := vec.(type) {
	case *vector.Const:
		typ := vec.Type()
		if super.IsFloat(typ.ID()) {
			v := vector.FloatValue(vec, 0)
			return vector.NewConstFloat(typ, -v, vec.Len()), true
		}
		v := vector.IntValue(vec, 0)
		if v == minInt(typ) {
			return nil, false
		}
		return vector.NewConstInt(typ, -v, vec.Len()), true
	case *vector.Dict:
		out, ok := u.convert(vec.Any)
		if !ok {
			return nil, false
		}
		return &vector.Dict{
			Any:    out,
			Index:  vec.Index,
			Counts: vec.Counts,
		}, true
	case *vector.View:
		out, ok := u.convert(vec.Any)
		if !ok {
			return nil, false
		}
		return &vector.View{Any: out, Index: vec.Index}, true
	case *vector.Int:
		min := minInt(vec.Type())
		out := make([]int64, vec.Len())
		for i := range vec.Len() {
			if vec.Values[i] == min {
				return nil, false
			}
			out[i] = -vec.Values[i]
		}
		return vector.NewInt(vec.Typ, out), true
	case *vector.Float:
		out := make([]float64, vec.Len())
		for i := range vec.Len() {
			out[i] = -vec.Values[i]
		}
		return vector.NewFloat(vec.Typ, out), true
	default:
		panic(vec)
	}
}

func (u *unaryMinus) slowPath(vec vector.Any) vector.Any {
	var ints []int64
	var errs []uint32
	minval := minInt(vec.Type())
	for i := range vec.Len() {
		v := vector.IntValue(vec, i)
		if v == minval {
			errs = append(errs, i)
		} else {
			ints = append(ints, -v)
		}
	}
	out := vector.NewInt(vec.Type(), ints)
	err := vector.NewWrappedError(u.sctx, "unary '-' underflow", vector.Pick(vec, errs))
	return vector.Combine(out, errs, err)
}

func minInt(typ super.Type) int64 {
	switch typ.ID() {
	case super.IDInt8:
		return math.MinInt8
	case super.IDInt16:
		return math.MinInt16
	case super.IDInt32:
		return math.MinInt32
	default:
		return math.MinInt64
	}
}
