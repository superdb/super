package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

// sum adds up the numeric values of each group.  The fast path handles
// values that all have the same state type (see numericStateType).  Anything
// else (strings or mixed types) switches to the slow path.  Errors and
// values of other types are ignored.
type sum struct {
	n      uint32
	typ    super.Type // nil until the first numeric value
	ints   []int64
	uints  []uint64
	floats []float64
	set    []bool
	slow   *slowMath
}

var _ expr.AggFunc = (*sum)(nil)

func newSum() *sum {
	return &sum{}
}

func (s *sum) Resize(n uint32) {
	s.n = n
	if s.slow != nil {
		s.slow.resize(n)
		return
	}
	if s.typ == nil {
		return
	}
	s.set = extend(s.set, n)
	switch {
	case s.ints != nil:
		s.ints = extend(s.ints, n)
	case s.uints != nil:
		s.uints = extend(s.uints, n)
	default:
		s.floats = extend(s.floats, n)
	}
}

func (s *sum) Consume(groups []uint32, vec vector.Any) {
	vec = vector.Under(vec)
	if vec.Kind() == vector.KindError {
		return
	}
	if s.slow != nil {
		s.slow.consume(groups, vec)
		return
	}
	typ := numericStateType(vec.Type())
	if typ == nil {
		if vec.Kind() == vector.KindString {
			// Strings don't sum but must be tracked to detect a mix
			// of strings and numbers.
			s.toSlow().consume(groups, vec)
		}
		return
	}
	if s.typ == nil {
		s.typ = typ
		switch id := typ.ID(); {
		case super.IsFloat(id):
			s.floats = make([]float64, s.n)
		case super.IsSigned(id):
			s.ints = make([]int64, s.n)
		default:
			s.uints = make([]uint64, s.n)
		}
		s.set = make([]bool, s.n)
	} else if s.typ != typ {
		s.toSlow().consume(groups, vec)
		return
	}
	flat, index := flatten(vec)
	switch flat := flat.(type) {
	case *vector.Int:
		addTo(s.ints, s.set, groups, flat.Values, index)
	case *vector.Uint:
		addTo(s.uints, s.set, groups, flat.Values, index)
	case *vector.Float:
		addTo(s.floats, s.set, groups, flat.Values, index)
	default:
		panic(flat)
	}
}

func addTo[T int64 | uint64 | float64](sums []T, set []bool, groups []uint32, vals []T, index []uint32) {
	for slot, g := range groups {
		i := uint32(slot)
		if index != nil {
			i = index[slot]
		}
		sums[g] += vals[i]
		set[g] = true
	}
}

// toSlow switches s to the slow path.
func (s *sum) toSlow() *slowMath {
	s.slow = newSlowMath(sumValues, s.n)
	if s.typ != nil {
		s.slow.load(s.fastResult())
	}
	s.ints, s.uints, s.floats, s.set = nil, nil, nil, nil
	return s.slow
}

func (s *sum) ConsumeAsPartial(groups []uint32, vec vector.Any) {
	if vector.Under(vec).Kind() == vector.KindError {
		// An error partial is a mixture of strings and numbers.
		if s.slow == nil {
			s.toSlow()
		}
		s.slow.markMixed(groups)
		return
	}
	s.Consume(groups, vec)
}

func (s *sum) Result(sctx *super.Context) vector.Any {
	if s.slow != nil {
		return s.slow.result(sctx, false)
	}
	return s.fastResult()
}

func (s *sum) fastResult() vector.Any {
	if s.typ == nil {
		return vector.NewNull(s.n)
	}
	var nulls []uint32
	for g, set := range s.set {
		if !set {
			nulls = append(nulls, uint32(g))
		}
	}
	var out vector.Any
	switch {
	case s.ints != nil:
		out = vector.NewInt(s.typ, compact(s.ints, s.set, len(nulls)))
	case s.uints != nil:
		out = vector.NewUint(s.typ, compact(s.uints, s.set, len(nulls)))
	default:
		out = vector.NewFloat(s.typ, compact(s.floats, s.set, len(nulls)))
	}
	return combineNulls(out, nulls, s.n)
}

// compact returns the elements of vals for which set is true.
func compact[T any](vals []T, set []bool, nnulls int) []T {
	if nnulls == 0 {
		return vals
	}
	out := make([]T, 0, len(vals)-nnulls)
	for g, ok := range set {
		if ok {
			out = append(out, vals[g])
		}
	}
	return out
}

func (s *sum) ResultAsPartial(sctx *super.Context) vector.Any {
	return s.Result(sctx)
}
