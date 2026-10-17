package function

import (
	"math"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/expr/cast"
	"github.com/superdb/super/vector"
)

type Abs struct {
	sctx *super.Context
}

func (a *Abs) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(a.sctx, true, args, "abs"); ok {
		return vec
	}
	vec := vector.Under(args[0])
	switch id := vec.Type().ID(); {
	case super.IsUnsigned(id):
		return vec
	case super.IsSigned(id) || super.IsFloat(id):
		return a.abs(vec)
	}
	return vector.NewWrappedError(a.sctx, "abs: not a number", vec)
}

func (a *Abs) abs(vec vector.Any) vector.Any {
	switch vec := vec.(type) {
	case *vector.Const:
		typ := vec.Type()
		if super.IsFloat(typ.ID()) {
			v := math.Abs(vector.FloatValue(vec, 0))
			return vector.NewConstFloat(typ, v, vec.Len())
		}
		v := vector.IntValue(vec, 0)
		if v < 0 {
			v = -v
		}
		return vector.NewConstInt(typ, v, vec.Len())
	case *vector.View:
		return vector.Pick(a.abs(vec.Any), vec.Index)
	case *vector.Dict:
		return vector.NewDict(a.abs(vec.Any), vec.Index, vec.Counts)
	case *vector.Int:
		var ints []int64
		for _, v := range vec.Values {
			if v < 0 {
				v = -v
			}
			ints = append(ints, v)
		}
		return vector.NewInt(vec.Type(), ints)
	case *vector.Float:
		var floats []float64
		for _, v := range vec.Values {
			floats = append(floats, math.Abs(v))
		}
		return vector.NewFloat(vec.Type(), floats)
	default:
		panic(vec)
	}
}

type Ceil struct {
	sctx *super.Context
}

func (c *Ceil) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(c.sctx, true, args, "ceil"); ok {
		return vec
	}
	vec := vector.Under(args[0])
	switch id := vec.Type().ID(); {
	case super.IsFloat(id):
		return c.ceil(vec)
	case super.IsNumber(id):
		return vec
	}
	return vector.NewWrappedError(c.sctx, "ceil: not a number", vec)
}

func (c *Ceil) ceil(vec vector.Any) vector.Any {
	switch vec := vec.(type) {
	case *vector.Const:
		v := math.Ceil(vector.FloatValue(vec, 0))
		return vector.NewConstFloat(vec.Type(), v, vec.Len())
	case *vector.View:
		return vector.Pick(c.ceil(vec.Any), vec.Index)
	case *vector.Dict:
		return vector.NewDict(c.ceil(vec.Any), vec.Index, vec.Counts)
	case *vector.Float:
		var floats []float64
		for _, v := range vec.Values {
			floats = append(floats, math.Ceil(v))
		}
		return vector.NewFloat(vec.Type(), floats)
	default:
		panic(vec)
	}
}

type Floor struct {
	sctx *super.Context
}

func (f *Floor) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(f.sctx, true, args, "floor"); ok {
		return vec
	}
	vec := vector.Under(args[0])
	switch id := vec.Type().ID(); {
	case super.IsFloat(id):
		return f.floor(vec)
	case super.IsNumber(id):
		return vec
	}
	return vector.NewWrappedError(f.sctx, "floor: not a number", vec)
}

func (f *Floor) floor(vec vector.Any) vector.Any {
	switch vec := vec.(type) {
	case *vector.Const:
		v := math.Floor(vector.FloatValue(vec, 0))
		return vector.NewConstFloat(vec.Type(), v, vec.Len())
	case *vector.View:
		return vector.Pick(f.floor(vec.Any), vec.Index)
	case *vector.Dict:
		return vector.NewDict(f.floor(vec.Any), vec.Index, vec.Counts)
	case *vector.Float:
		var floats []float64
		for _, v := range vec.Values {
			floats = append(floats, math.Floor(v))
		}
		return vector.NewFloat(vec.Type(), floats)
	default:
		panic(vec)
	}
}

type Log struct {
	sctx *super.Context
}

func (l *Log) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(l.sctx, true, args, "log"); ok {
		return vec
	}
	arg := vector.Under(args[0])
	if !super.IsNumber(arg.Type().ID()) {
		return vector.NewWrappedError(l.sctx, "log: not a number", arg)
	}
	// No error casting number to float so no need to Apply.
	vec := cast.To(l.sctx, arg, super.TypeFloat64)
	var errs []uint32
	var floats []float64
	for i := range vec.Len() {
		v := vector.FloatValue(vec, i)
		if v <= 0 {
			errs = append(errs, i)
			continue
		}
		floats = append(floats, math.Log(v))
	}
	out := vector.NewFloat(super.TypeFloat64, floats)
	if len(errs) > 0 {
		err := vector.NewWrappedError(l.sctx, "log: illegal argument", vector.Pick(arg, errs))
		return vector.Combine(out, errs, err)
	}
	return out
}

type Pow struct {
	sctx *super.Context
}

func (p *Pow) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(p.sctx, true, args, "pow"); ok {
		return vec
	}
	a, b := vector.Under(args[0]), vector.Under(args[1])
	if !super.IsNumber(a.Type().ID()) {
		return vector.NewWrappedError(p.sctx, "pow: not a number", args[0])
	}
	if !super.IsNumber(b.Type().ID()) {
		return vector.NewWrappedError(p.sctx, "pow: not a number", args[1])
	}
	a = cast.To(p.sctx, a, super.TypeFloat64)
	b = cast.To(p.sctx, b, super.TypeFloat64)
	vals := make([]float64, a.Len())
	for i := range a.Len() {
		x := vector.FloatValue(a, i)
		y := vector.FloatValue(b, i)
		vals[i] = math.Pow(x, y)
	}
	return vector.NewFloat(super.TypeFloat64, vals)
}

type Round struct {
	sctx *super.Context
}

func (r *Round) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(r.sctx, true, args, "round"); ok {
		return vec
	}
	vec := args[0]
	switch id := vec.Type().ID(); {
	case super.IsFloat(id):
		vals := make([]float64, vec.Len())
		for i := range vec.Len() {
			v := vector.FloatValue(vec, i)
			vals[i] = math.Round(v)
		}
		return vector.NewFloat(vec.Type(), vals)
	case super.IsNumber(id):
		return vec
	}
	return vector.NewWrappedError(r.sctx, "round: not a number", vec)
}

type Sqrt struct {
	sctx *super.Context
}

func (s *Sqrt) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(s.sctx, true, args, "sqrt"); ok {
		return vec
	}
	vec := vector.Under(args[0])
	if !super.IsNumber(vec.Type().ID()) {
		return vector.NewWrappedError(s.sctx, "sqrt: number argument required", vec)
	}
	vec = cast.To(s.sctx, vec, super.TypeFloat64)
	vals := make([]float64, vec.Len())
	for i := range vec.Len() {
		v := vector.FloatValue(vec, i)
		vals[i] = math.Sqrt(v)
	}
	return vector.NewFloat(super.TypeFloat64, vals)
}
