package agg

import (
	"bytes"
	"cmp"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

// minMax computes the min (sign < 0) or max (sign > 0) of the numeric or
// string values of each group.  The fast path handles values that all have
// the same state type (a string or see numericStateType).  Mixed types switch
// to the slow path.  Errors and values of other types are ignored.
type minMax struct {
	sign  int
	n     uint32
	state minMaxState // nil until the first value
	slow  *slowMath
}

type minMaxState interface {
	typ() super.Type
	resize(n uint32)
	consume(groups []uint32, vec vector.Any)
	result() vector.Any
}

var _ expr.AggFunc = (*minMax)(nil)

func newMin() *minMax { return &minMax{sign: -1} }
func newMax() *minMax { return &minMax{sign: 1} }

func (m *minMax) Resize(n uint32) {
	m.n = n
	if m.slow != nil {
		m.slow.resize(n)
	} else if m.state != nil {
		m.state.resize(n)
	}
}

func (m *minMax) Consume(groups []uint32, vec vector.Any) {
	vec = vector.Under(vec)
	if vec.Kind() == vector.KindError {
		return
	}
	if m.slow != nil {
		m.slow.consume(groups, vec)
		return
	}
	typ := vec.Type()
	if typ.ID() != super.IDString {
		if typ = numericStateType(typ); typ == nil {
			return
		}
	}
	if m.state == nil {
		m.state = m.newState(typ)
		m.state.resize(m.n)
	} else if m.state.typ() != typ {
		m.toSlow().consume(groups, vec)
		return
	}
	m.state.consume(groups, vec)
}

func (m *minMax) newState(typ super.Type) minMaxState {
	switch id := typ.ID(); {
	case id == super.IDString:
		return &stringMinMax{sign: m.sign}
	case super.IsFloat(id):
		return &numericMinMax[float64]{t: typ, sign: m.sign}
	case super.IsSigned(id):
		return &numericMinMax[int64]{t: typ, sign: m.sign}
	default:
		return &numericMinMax[uint64]{t: typ, sign: m.sign}
	}
}

// toSlow switches m to the slow path.
func (m *minMax) toSlow() *slowMath {
	m.slow = newSlowMath(minMaxValues(m.sign), m.n)
	if m.state != nil {
		m.slow.load(m.state.result())
		m.state = nil
	}
	return m.slow
}

func (m *minMax) ConsumeAsPartial(groups []uint32, vec vector.Any) {
	if vector.Under(vec).Kind() == vector.KindError {
		// An error partial is a mixture of strings and numbers.
		if m.slow == nil {
			m.toSlow()
		}
		m.slow.markMixed(groups)
		return
	}
	m.Consume(groups, vec)
}

func (m *minMax) Result(sctx *super.Context) vector.Any {
	if m.slow != nil {
		return m.slow.result(sctx, true)
	}
	if m.state == nil {
		return vector.NewNull(m.n)
	}
	return m.state.result()
}

func (m *minMax) ResultAsPartial(sctx *super.Context) vector.Any {
	return m.Result(sctx)
}

type numericMinMax[T int64 | uint64 | float64] struct {
	t    super.Type
	sign int
	vals []T
	set  []bool
}

func (s *numericMinMax[T]) typ() super.Type { return s.t }

func (s *numericMinMax[T]) resize(n uint32) {
	s.vals = extend(s.vals, n)
	s.set = extend(s.set, n)
}

func (s *numericMinMax[T]) consume(groups []uint32, vec vector.Any) {
	flat, index := flatten(vec)
	var vals []T
	switch flat := flat.(type) {
	case *vector.Int:
		vals = any(flat.Values).([]T)
	case *vector.Uint:
		vals = any(flat.Values).([]T)
	case *vector.Float:
		vals = any(flat.Values).([]T)
	default:
		panic(flat)
	}
	for slot, g := range groups {
		i := uint32(slot)
		if index != nil {
			i = index[slot]
		}
		if v := vals[i]; !s.set[g] || compareNaNLast(v, s.vals[g]) == s.sign {
			s.vals[g] = v
			s.set[g] = true
		}
	}
}

func (s *numericMinMax[T]) result() vector.Any {
	vals := make([]T, 0, len(s.vals))
	var nulls []uint32
	for g, set := range s.set {
		if set {
			vals = append(vals, s.vals[g])
		} else {
			nulls = append(nulls, uint32(g))
		}
	}
	var out vector.Any
	switch vals := any(vals).(type) {
	case []int64:
		out = vector.NewInt(s.t, vals)
	case []uint64:
		out = vector.NewUint(s.t, vals)
	case []float64:
		out = vector.NewFloat(s.t, vals)
	}
	return combineNulls(out, nulls, uint32(len(s.vals)))
}

type stringMinMax struct {
	sign int
	vals [][]byte
	set  []bool
}

func (s *stringMinMax) typ() super.Type { return super.TypeString }

func (s *stringMinMax) resize(n uint32) {
	s.vals = extend(s.vals, n)
	s.set = extend(s.set, n)
}

func (s *stringMinMax) consume(groups []uint32, vec vector.Any) {
	flat, index := flatten(vec)
	table := flat.(*vector.String).Table()
	for slot, g := range groups {
		i := uint32(slot)
		if index != nil {
			i = index[slot]
		}
		if b := table.Bytes(i); !s.set[g] || bytes.Compare(b, s.vals[g]) == s.sign {
			s.vals[g] = slices.Clone(b)
			s.set[g] = true
		}
	}
}

func (s *stringMinMax) result() vector.Any {
	table := vector.NewBytesTableEmpty(uint32(len(s.vals)))
	var nulls []uint32
	for g, set := range s.set {
		if set {
			table.Append(s.vals[g])
		} else {
			nulls = append(nulls, uint32(g))
		}
	}
	return combineNulls(vector.NewString(table), nulls, uint32(len(s.vals)))
}

// compareNaNLast is like cmp.Compare but orders NaN after all other values
// (as Postgres and DuckDB do), so max returns NaN and min ignores it.
func compareNaNLast[T cmp.Ordered](a, b T) int {
	aNaN, bNaN := a != a, b != b
	switch {
	case aNaN && bNaN:
		return 0
	case aNaN:
		return 1
	case bNaN:
		return -1
	}
	return cmp.Compare(a, b)
}
