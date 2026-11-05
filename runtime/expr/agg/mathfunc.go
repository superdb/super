package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/pkg/anymath"
	"github.com/superdb/super/vector"
	"golang.org/x/exp/constraints"
)

var mathSum = &mathFunc{
	op:          opSum,
	Init:        anymath.Add.Init,
	funcFloat64: sum[float64],
	funcInt64:   sum[int64],
	funcUint64:  sum[uint64],
}

var mathMin = &mathFunc{
	op:          opMin,
	Init:        anymath.Min.Init,
	funcFloat64: minNumeric[float64],
	funcInt64:   minNumeric[int64],
	funcUint64:  minNumeric[uint64],
	funcString:  minString,
}

var mathMax = &mathFunc{
	op:          opMax,
	Init:        anymath.Max.Init,
	funcFloat64: maxNumeric[float64],
	funcInt64:   maxNumeric[int64],
	funcUint64:  maxNumeric[uint64],
	funcString:  maxString,
}

type funcFloat64 func(float64, vector.Any) float64
type funcInt64 func(int64, vector.Any) int64
type funcUint64 func(uint64, vector.Any) uint64
type funcString func(string, vector.Any) string

type mathOp int

const (
	opSum mathOp = iota
	opMin
	opMax
)

type mathFunc struct {
	op mathOp
	anymath.Init
	funcFloat64
	funcInt64
	funcUint64
	funcString
}

type numeric interface {
	constraints.Integer | constraints.Float
}

func sum[T numeric](state T, vec vector.Any) T {
	switch vec := vec.(type) {
	case *vector.Const:
		v := constToNumeric[T](vec)
		return state + v*T(vec.Len())
	case *vector.Dict:
		return sumFlat(state, vec.Any, nil, vec.Counts)
	case *vector.View:
		return sumFlat(state, vec.Any, vec.Index, nil)
	default:
		return sumFlat(state, vec, nil, nil)
	}
}

func sumFlat[T numeric](state T, vec vector.Any, index []uint32, counts []uint32) T {
	switch vec := vec.(type) {
	case *vector.Uint:
		return sumOf(state, vec.Values, index, counts)
	case *vector.Int:
		return sumOf(state, vec.Values, index, counts)
	case *vector.Float:
		return sumOf(state, vec.Values, index, counts)
	default:
		panic(vec)
	}
}

func sumOf[T numeric, E numeric](state T, vals []E, index []uint32, counts []uint32) T {
	if index != nil {
		for _, idx := range index {
			state += T(vals[idx])
		}
		return state
	}
	if counts != nil {
		for i, count := range counts {
			state += T(vals[i]) * T(count)
		}
		return state
	}
	for _, v := range vals {
		state += T(v)
	}
	return state
}

func minNumeric[T numeric](state T, vec vector.Any) T {
	switch vec := vec.(type) {
	case *vector.Const:
		return min(state, constToNumeric[T](vec))
	case *vector.Dict:
		return minFlat(state, vec.Any, nil)
	case *vector.View:
		return minFlat(state, vec.Any, vec.Index)
	default:
		return minFlat(state, vec, nil)
	}
}

func minFlat[T numeric](state T, vec vector.Any, index []uint32) T {
	switch vec := vec.(type) {
	case *vector.Uint:
		return minOf(state, vec.Values, index)
	case *vector.Int:
		return minOf(state, vec.Values, index)
	case *vector.Float:
		return minOf(state, vec.Values, index)
	default:
		panic(vec)
	}
}

func minOf[T numeric, E numeric](state T, vals []E, index []uint32) T {
	if index != nil {
		for _, idx := range index {
			state = min(state, T(vals[idx]))
		}
		return state
	}
	for _, v := range vals {
		state = min(state, T(v))
	}
	return state
}

func maxNumeric[T numeric](state T, vec vector.Any) T {
	switch vec := vec.(type) {
	case *vector.Const:
		return max(state, constToNumeric[T](vec))
	case *vector.Dict:
		return maxFlat(state, vec.Any, nil)
	case *vector.View:
		return maxFlat(state, vec.Any, vec.Index)
	default:
		return maxFlat(state, vec, nil)
	}
}

func maxFlat[T numeric](state T, vec vector.Any, index []uint32) T {
	switch vec := vec.(type) {
	case *vector.Uint:
		return maxOf(state, vec.Values, index)
	case *vector.Int:
		return maxOf(state, vec.Values, index)
	case *vector.Float:
		return maxOf(state, vec.Values, index)
	default:
		panic(vec)
	}
}

func maxOf[T numeric, E numeric](state T, vals []E, index []uint32) T {
	if index != nil {
		for _, idx := range index {
			state = max(state, T(vals[idx]))
		}
		return state
	}
	for _, v := range vals {
		state = max(state, T(v))
	}
	return state
}

func constToNumeric[T numeric](vec *vector.Const) T {
	switch id := vec.Type().ID(); {
	case super.IsUnsigned(id):
		return T(vector.UintValue(vec, 0))
	case super.IsSigned(id):
		return T(vector.IntValue(vec, 0))
	default:
		return T(vector.FloatValue(vec, 0))
	}
}

func minString(state string, vec vector.Any) string {
	if vec, ok := vec.(*vector.Const); ok {
		return min(state, vector.StringValue(vec, 0))
	}
	for i := range vec.Len() {
		state = min(state, vector.StringValue(vec, i))
	}
	return state
}

func maxString(state string, vec vector.Any) string {
	switch vec := vec.(type) {
	case *vector.Const:
		return max(state, vector.StringValue(vec, 0))
	default:
		for i := range vec.Len() {
			state = max(state, vector.StringValue(vec, i))
		}
		return state
	}
}
