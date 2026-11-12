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
	// consumeAll is like consume with every slot of non-empty vec in
	// group 0.
	consumeAll(vec vector.Any)
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
	m.consume(groups, vec, false)
}

func (m *minMax) ConsumeAll(vec vector.Any) {
	m.consume(nil, vec, true)
}

// consume is Consume or, if all is true, ConsumeAll, ignoring groups.  Only
// the slow path needs a slice of zero groups for all.
func (m *minMax) consume(groups []uint32, vec vector.Any, all bool) {
	vec = vector.Under(vec)
	if vec.Kind() == vector.KindError {
		return
	}
	if m.slow != nil {
		m.slow.consume(groupsOrZeros(groups, all, vec.Len()), vec)
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
		m.toSlow().consume(groupsOrZeros(groups, all, vec.Len()), vec)
		return
	}
	if all {
		m.state.consumeAll(vec)
	} else {
		m.state.consume(groups, vec)
	}
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
	vals := s.values(flat)
	for slot, g := range groups {
		i := uint32(slot)
		if index != nil {
			i = index[slot]
		}
		if !s.set[g] {
			s.vals[g] = vals[i]
			s.set[g] = true
			continue
		}
		if v := vals[i]; compareNaNLast(v, s.vals[g]) == s.sign {
			s.vals[g] = v
			s.set[g] = true
		}
	}
}

func (s *numericMinMax[T]) consumeAll(vec vector.Any) {
	if !s.set[0] {
		s.vals[0] = s.firstValue(vec)
		s.set[0] = true
	}
	best := s.vals[0]
	switch vec := vec.(type) {
	case *vector.Const:
		best = s.better(s.values(vec.Any)[0], best)
	case *vector.Dict:
		vals := s.values(vec.Any)
		for k, count := range vec.Counts {
			if count > 0 {
				best = s.better(vals[k], best)
			}
		}
	case *vector.View:
		vals := s.values(vec.Any)
		for _, i := range vec.Index {
			best = s.better(vals[i], best)
		}
	default:
		vals := s.values(vec)
		for i := range vec.Len() {
			best = s.better(vals[i], best)
		}
	}
	s.vals[0] = best
}

// better returns v if compareNaNLast(v, best) == s.sign and best otherwise.
func (s *numericMinMax[T]) better(v, best T) T {
	if s.sign < 0 {
		if v < best || (best != best && v == v) {
			return v
		}
	} else if v > best || v != v {
		return v
	}
	return best
}

// values returns the values of flat numeric vector vec.
func (s *numericMinMax[T]) values(vec vector.Any) []T {
	switch vec := vec.(type) {
	case *vector.Int:
		return any(vec.Values).([]T)
	case *vector.Uint:
		return any(vec.Values).([]T)
	case *vector.Float:
		return any(vec.Values).([]T)
	default:
		panic(vec)
	}
}

func (s *numericMinMax[T]) firstValue(vec vector.Any) T {
	switch vec.Kind() {
	case vector.KindInt:
		return T(vector.IntValue(vec, 0))
	case vector.KindUint:
		return T(vector.UintValue(vec, 0))
	case vector.KindFloat:
		return T(vector.FloatValue(vec, 0))
	default:
		panic(vec)
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

func (s *stringMinMax) consumeAll(vec vector.Any) {
	if !s.set[0] {
		s.vals[0] = []byte(vector.StringValue(vec, 0))
		s.set[0] = true
	}
	best := s.vals[0]
	switch vec := vec.(type) {
	case *vector.Const:
		v := vec.Any.(*vector.String).Table().Bytes(0)
		if bytes.Compare(v, best) == s.sign {
			best = v
		}
	case *vector.Dict:
		table := vec.Any.(*vector.String).Table()
		for k, count := range vec.Counts {
			if count > 0 && bytes.Compare(table.Bytes(uint32(k)), best) == s.sign {
				best = table.Bytes(uint32(k))
			}
		}
	case *vector.View:
		table := vec.Any.(*vector.String).Table()
		for _, idx := range vec.Index {
			if b := table.Bytes(idx); bytes.Compare(b, best) == s.sign {
				best = b
			}
		}
	default:
		table := vec.(*vector.String).Table()
		for k := range vec.Len() {
			if b := table.Bytes(k); bytes.Compare(b, best) == s.sign {
				best = b
			}
		}
	}
	if !bytes.Equal(s.vals[0], best) {
		s.vals[0] = slices.Clone(best)
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
