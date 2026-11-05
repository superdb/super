package expr

//go:generate go run genarithfuncs.go

import (
	"fmt"
	"runtime"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr/cast"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

type Arith struct {
	sctx   *super.Context
	op     string
	lhs    Evaluator
	rhs    Evaluator
	opCode int
}

func NewArith(sctx *super.Context, op string, lhs, rhs Evaluator) *Arith {
	return &Arith{sctx, op, lhs, rhs, vector.ArithOpFromString(op)}
}

func (a *Arith) Eval(val vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipUnions|vector.ApplyRipFusions, a.eval, a.lhs.Eval(val), a.rhs.Eval(val))
}

func (a *Arith) eval(vecs ...vector.Any) (out vector.Any) {
	if vec, ok := CatchFirst(a.sctx, CatchAll, vecs, a.op); ok {
		return vec
	}
	lhs := vector.Under(vecs[0])
	rhs := vector.Under(vecs[1])
	// The difference of two timestamps is a duration.
	timeDiff := a.opCode == vector.ArithSub &&
		lhs.Type().ID() == super.IDTimestamp && rhs.Type().ID() == super.IDTimestamp
	lhs, rhs, errVal := coerceVals(a.sctx, lhs, rhs)
	if errVal != nil {
		return errVal
	}
	kind := lhs.Kind()
	if kind != rhs.Kind() {
		// Coercion yields errors for values that overflow the target
		// type (e.g., a uint64 too large for int64).  Apply again so
		// those slots produce errors and the others are computed.
		if lhs.Kind() == vector.KindError || rhs.Kind() == vector.KindError ||
			isDynamic(lhs) || isDynamic(rhs) {
			return vector.Apply(vector.ApplyRipUnions, a.eval, lhs, rhs)
		}
		panic(fmt.Sprintf("vector kind mismatch after coerce (%#v and %#v)", lhs, rhs))
	}
	if kind == vector.KindFloat && a.opCode == vector.ArithMod {
		return a.incompatibleError(lhs)
	}
	lform, ok := vector.FormOf(lhs)
	if !ok {
		return a.incompatibleError(lhs)
	}
	rform, ok := vector.FormOf(rhs)
	if !ok {
		return a.incompatibleError(rhs)
	}
	f, ok := arithFuncs[vector.FuncCode(a.opCode, kind, lform, rform)]
	if !ok {
		return a.incompatibleError(lhs)
	}
	if a.opCode == vector.ArithDiv || a.opCode == vector.ArithMod {
		defer func() {
			if v := recover(); v != nil {
				if err, ok := v.(runtime.Error); ok && err.Error() == "runtime error: integer divide by zero" {
					out = a.evalDivideByZero(kind, lhs, rhs)
				} else {
					panic(v)
				}
			}
		}()
	}
	if timeDiff {
		out = f(lhs, rhs)
		if vec, ok := out.(*vector.Int); ok {
			// f made a new vector so relabel it instead of copying it.
			return vector.NewInt(super.TypeDuration, vec.Values)
		}
		return cast.To(a.sctx, out, super.TypeDuration)
	}
	return f(lhs, rhs)
}

func (a *Arith) incompatibleError(vec vector.Any) vector.Any {
	msg := fmt.Sprintf("type %s incompatible with '%s' operator", sup.FormatType(vec.Type()), a.op)
	return vector.NewStringError(a.sctx, msg, vec.Len())
}

func (a *Arith) evalDivideByZero(kind vector.Kind, lhs, rhs vector.Any) vector.Any {
	var errs []uint32
	var out vector.Any
	switch kind {
	case vector.KindInt:
		var ints []int64
		for i := range lhs.Len() {
			r := vector.IntValue(rhs, i)
			if r == 0 {
				errs = append(errs, i)
				continue
			}
			l := vector.IntValue(lhs, i)
			if a.opCode == vector.ArithDiv {
				ints = append(ints, l/r)
			} else {
				ints = append(ints, l%r)
			}
		}
		out = vector.NewInt(super.TypeInt64, ints)
	case vector.KindUint:
		var uints []uint64
		for i := range lhs.Len() {
			r := vector.UintValue(rhs, i)
			if r == 0 {
				errs = append(errs, i)
				continue
			}
			l := vector.UintValue(lhs, i)
			if a.opCode == vector.ArithDiv {
				uints = append(uints, l/r)
			} else {
				uints = append(uints, l%r)
			}
		}
		out = vector.NewUint(super.TypeUint64, uints)
	default:
		panic(kind)
	}
	if len(errs) > 0 {
		return vector.Combine(out, errs, vector.NewStringError(a.sctx, "divide by zero", uint32(len(errs))))
	}
	return out
}

func isDynamic(vec vector.Any) bool {
	_, ok := vec.(*vector.Dynamic)
	return ok
}
