package agg

import (
	"fmt"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/sam/expr/coerce"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

// mathTag is the state of a group in a mathReducer: which kind of value it
// has accumulated, if any.
type mathTag uint8

const (
	tagNone mathTag = iota
	tagUint
	tagInt
	tagDuration
	tagTimestamp
	tagFloat
	tagString
	tagMixed // mixture of string and numeric values
	numMathTags
)

// mathReducer computes sum, min, or max for each group.  Each group keeps
// its own state type, which is promoted as values of wider types arrive,
// so the result type of a group depends only on the values in that group.
type mathReducer struct {
	function *mathFunc
	tags     []mathTag
	// State by group, allocated on first use.  ints holds the state of
	// int, duration, and timestamp groups.
	ints   []int64
	uints  []uint64
	floats []float64
	strs   []string
}

func newMathReducer(f *mathFunc) *mathReducer {
	return &mathReducer{function: f}
}

var _ expr.AggFunc = (*mathReducer)(nil)

func (m *mathReducer) Resize(n uint32) {
	m.tags = append(m.tags, make([]mathTag, n-uint32(len(m.tags)))...)
	m.ints = grow(m.ints, n)
	m.uints = grow(m.uints, n)
	m.floats = grow(m.floats, n)
	m.strs = grow(m.strs, n)
}

func (m *mathReducer) Consume(groups []uint32, vec vector.Any) {
	vec = vector.Under(vec)
	typ := vec.Type()
	switch {
	case typ == super.TypeString:
		m.consumeString(groups, vec)
	case super.IsNumber(typ.ID()):
		m.consumeNumeric(groups, vec)
	}
}

func (m *mathReducer) ConsumeAsPartial(groups []uint32, vec vector.Any) {
	if vec.Kind() == vector.KindError {
		for _, g := range orZeros(groups, vec.Len()) {
			m.tags[g] = tagMixed
		}
		return
	}
	m.Consume(groups, vec)
}

func (m *mathReducer) consumeString(groups []uint32, vec vector.Any) {
	f := m.function.funcString
	if groups == nil {
		switch m.tags[0] {
		case tagNone:
			m.tags[0] = tagString
			if f != nil {
				m.alloc(tagString)
				m.strs[0] = f(vector.StringValue(vec, 0), vec)
			}
		case tagString:
			if f != nil {
				m.strs[0] = f(m.strs[0], vec)
			}
		case tagMixed:
		default:
			m.tags[0] = tagMixed
		}
		return
	}
	if f != nil {
		m.alloc(tagString)
	}
	for i, g := range groups {
		switch m.tags[g] {
		case tagNone:
			m.tags[g] = tagString
			if f != nil {
				m.strs[g] = vector.StringValue(vec, uint32(i))
			}
		case tagString:
			if f != nil {
				s := vector.StringValue(vec, uint32(i))
				if m.function.op == opMin {
					m.strs[g] = min(m.strs[g], s)
				} else {
					m.strs[g] = max(m.strs[g], s)
				}
			}
		case tagMixed:
		default:
			m.tags[g] = tagMixed
		}
	}
}

// transition is the new state of a group in the old state given numeric
// input of some type, with ok false when the input is to be skipped.
type transition struct {
	tag mathTag
	ok  bool
}

func (m *mathReducer) transition(old mathTag, typ super.Type) transition {
	switch old {
	case tagNone:
		return transition{tagOfNumber(typ.ID()), true}
	case tagString:
		return transition{tagMixed, false}
	case tagMixed:
		return transition{tagMixed, false}
	}
	id, err := coerce.Promote(super.NewValue(promoteType(old), nil), super.NewValue(typ, nil))
	if err != nil {
		// Skip invalid values.
		return transition{old, false}
	}
	return transition{tagOfNumber(id), true}
}

func (m *mathReducer) consumeNumeric(groups []uint32, vec vector.Any) {
	typ := vec.Type()
	if groups == nil {
		t := m.transition(m.tags[0], typ)
		m.setTag(0, t.tag)
		if !t.ok {
			return
		}
		switch t.tag {
		case tagUint:
			m.uints[0] = m.function.funcUint64(m.uints[0], vec)
		case tagFloat:
			m.floats[0] = m.function.funcFloat64(m.floats[0], vec)
		default:
			m.ints[0] = m.function.funcInt64(m.ints[0], vec)
		}
		return
	}
	// Move each group to its new state, noting which states take input.
	var transitions [numMathTags]*transition
	var targets [numMathTags]bool
	for _, g := range groups {
		old := m.tags[g]
		t := transitions[old]
		if t == nil {
			t = new(m.transition(old, typ))
			transitions[old] = t
		}
		if t.tag != old {
			m.setTag(g, t.tag)
		}
		if t.ok {
			targets[t.tag] = true
		}
	}
	// Fold the input into each state that takes it.  A group's tag is a
	// target only if it took input, since transitions are idempotent.
	base, index := flatten(vec)
	for tag, ok := range targets {
		if !ok {
			continue
		}
		tag := mathTag(tag)
		switch tag {
		case tagUint:
			foldNumeric(m.function.op, m.uints, m.tags, tag, groups, base, index)
		case tagFloat:
			foldNumeric(m.function.op, m.floats, m.tags, tag, groups, base, index)
		default:
			foldNumeric(m.function.op, m.ints, m.tags, tag, groups, base, index)
		}
	}
}

// setTag moves group g to state tag, converting its state value.
func (m *mathReducer) setTag(g uint32, tag mathTag) {
	old := m.tags[g]
	if old == tag {
		return
	}
	m.tags[g] = tag
	if tag == tagMixed {
		return
	}
	m.alloc(tag)
	init := m.function.Init
	var val super.Value
	switch old {
	case tagNone:
		switch tag {
		case tagUint:
			m.uints[g] = init.Uint64
		case tagFloat:
			m.floats[g] = init.Float64
		default:
			m.ints[g] = init.Int64
		}
		return
	case tagUint:
		val = super.NewUint(super.TypeUint64, m.uints[g])
	case tagFloat:
		val = super.NewFloat64(m.floats[g])
	default:
		val = super.NewInt(resultType(old), m.ints[g])
	}
	var ok bool
	switch tag {
	case tagUint:
		m.uints[g], ok = coerce.ToUint(val, super.TypeUint64)
	case tagFloat:
		m.floats[g], ok = coerce.ToFloat(val, super.TypeFloat64)
	default:
		m.ints[g], ok = coerce.ToInt(val, resultType(tag))
	}
	if !ok {
		panicCoercionFail(resultType(tag), val.Type())
	}
}

func (m *mathReducer) alloc(tag mathTag) {
	n := len(m.tags)
	switch tag {
	case tagUint:
		if m.uints == nil {
			m.uints = make([]uint64, n)
		}
	case tagFloat:
		if m.floats == nil {
			m.floats = make([]float64, n)
		}
	case tagString:
		if m.strs == nil {
			m.strs = make([]string, n)
		}
	case tagInt, tagDuration, tagTimestamp:
		if m.ints == nil {
			m.ints = make([]int64, n)
		}
	}
}

// foldNumeric folds the numeric values of vec (a flat vector, with index
// mapping slots to it) into the state of their groups.  If tags is
// non-nil, only groups with tag want are folded.
func foldNumeric[S numeric](op mathOp, state []S, tags []mathTag, want mathTag, groups []uint32, vec vector.Any, index []uint32) {
	switch vec := vec.(type) {
	case *vector.Uint:
		foldValues(op, state, tags, want, groups, vec.Values, index)
	case *vector.Int:
		foldValues(op, state, tags, want, groups, vec.Values, index)
	case *vector.Float:
		foldValues(op, state, tags, want, groups, vec.Values, index)
	default:
		panic(vec)
	}
}

func foldValues[S, V numeric](op mathOp, state []S, tags []mathTag, want mathTag, groups []uint32, vals []V, index []uint32) {
	for i, g := range groups {
		if tags != nil && tags[g] != want {
			continue
		}
		k := i
		if index != nil {
			k = int(index[i])
		}
		v := S(vals[k])
		switch op {
		case opSum:
			state[g] += v
		case opMin:
			state[g] = min(state[g], v)
		case opMax:
			state[g] = max(state[g], v)
		}
	}
}

func (m *mathReducer) Result(sctx *super.Context) vector.Any {
	return buildByTag(m.tags, int(numMathTags), func(tag mathTag, groups []uint32) vector.Any {
		n := uint32(len(m.tags))
		if groups != nil {
			n = uint32(len(groups))
		}
		switch tag {
		case tagNone:
			return vector.NewNull(n)
		case tagMixed:
			return vector.NewStringError(sctx, "mixture of string and numeric values", n)
		case tagString:
			if m.function.funcString == nil {
				return vector.NewNull(n)
			}
			out := vector.NewStringEmpty(n)
			for _, s := range pick(m.strs, groups) {
				out.Append(s)
			}
			return out
		case tagUint:
			return vector.NewUint(super.TypeUint64, pick(m.uints, groups))
		case tagFloat:
			return vector.NewFloat(super.TypeFloat64, pick(m.floats, groups))
		default:
			return vector.NewInt(resultType(tag), pick(m.ints, groups))
		}
	})
}

func (m *mathReducer) ResultAsPartial(sctx *super.Context) vector.Any {
	return m.Result(sctx)
}

// pick returns the elements of s at index, or all of s if index is nil.
func pick[E any](s []E, index []uint32) []E {
	if index == nil {
		return s
	}
	out := make([]E, len(index))
	for i, k := range index {
		out[i] = s[k]
	}
	return out
}

func tagOfNumber(id int) mathTag {
	switch id {
	case super.IDUint8, super.IDUint16, super.IDUint32, super.IDUint64:
		return tagUint
	case super.IDInt8, super.IDInt16, super.IDInt32, super.IDInt64:
		return tagInt
	case super.IDDuration:
		return tagDuration
	case super.IDTimestamp:
		return tagTimestamp
	case super.IDFloat16, super.IDFloat32, super.IDFloat64:
		return tagFloat
	default:
		panic(id)
	}
}

// promoteType is the type used to promote a group's state with new input.
// Note that int, duration, and timestamp states all promote as int64.
func promoteType(tag mathTag) super.Type {
	switch tag {
	case tagUint:
		return super.TypeUint64
	case tagFloat:
		return super.TypeFloat64
	default:
		return super.TypeInt64
	}
}

func resultType(tag mathTag) super.Type {
	switch tag {
	case tagUint:
		return super.TypeUint64
	case tagInt:
		return super.TypeInt64
	case tagDuration:
		return super.TypeDuration
	case tagTimestamp:
		return super.TypeTimestamp
	case tagFloat:
		return super.TypeFloat64
	default:
		panic(tag)
	}
}

func panicCoercionFail(to, from super.Type) {
	panic(fmt.Sprintf("internal aggregation error: cannot coerce %s to %s", sup.String(from), sup.String(to)))
}
