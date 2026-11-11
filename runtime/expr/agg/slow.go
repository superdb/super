package agg

import (
	"bytes"
	"cmp"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/sam/expr/coerce"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

// slowMath is the slow path for sum, min, and max, used once values of
// different types (or partial errors) arrive.  Each group's state is a
// super.Value so groups can have different types.  Numeric values are
// promoted as needed; a group with both string and numeric values becomes
// an error.  Nulls, errors, and values of other types are ignored.
type slowMath struct {
	// combine reduces two non-null values of the same state type.
	combine func(a, b super.Value) super.Value
	vals    []super.Value
	set     []bool
	mixed   []bool
	builder scode.Builder
}

func newSlowMath(combine func(a, b super.Value) super.Value, n uint32) *slowMath {
	s := &slowMath{combine: combine}
	s.resize(n)
	return s
}

func (s *slowMath) resize(n uint32) {
	s.vals = extend(s.vals, n)
	s.set = extend(s.set, n)
	s.mixed = extend(s.mixed, n)
}

// load consumes the result of a fast path, i.e., a vector with one slot per
// group in which a null means the group has no value.
func (s *slowMath) load(vec vector.Any) {
	groups := make([]uint32, vec.Len())
	for i := range groups {
		groups[i] = uint32(i)
	}
	s.consume(groups, vec)
}

func (s *slowMath) consume(groups []uint32, vec vector.Any) {
	for slot, g := range groups {
		if !s.mixed[g] {
			s.consumeValue(g, vector.ValueAt(&s.builder, vec, uint32(slot)))
		}
	}
}

func (s *slowMath) consumeValue(g uint32, val super.Value) {
	val = val.Under()
	if val.IsNull() {
		return
	}
	id := val.Type().ID()
	if id != super.IDString {
		typ := numericStateType(val.Type())
		if typ == nil {
			// Ignore errors and non-numeric types.
			return
		}
		val = toStateType(val, typ)
	}
	if !s.set[g] {
		s.vals[g] = val.Copy()
		s.set[g] = true
		return
	}
	cur := s.vals[g]
	if (id == super.IDString) != (cur.Type().ID() == super.IDString) {
		s.mixed[g] = true
		return
	}
	if id != super.IDString && cur.Type() != val.Type() {
		pid, err := coerce.Promote(cur, val)
		if err != nil {
			// Skip values that can't be promoted (e.g., uint64 overflow).
			return
		}
		ptyp, err := super.LookupPrimitiveByID(pid)
		if err != nil {
			panic(err)
		}
		typ := numericStateType(ptyp)
		cur, val = toStateType(cur, typ), toStateType(val, typ)
	}
	s.vals[g] = s.combine(cur, val).Copy()
}

// markMixed marks groups as having mixed types, e.g., because a partial
// result for them is an error.
func (s *slowMath) markMixed(groups []uint32) {
	for _, g := range groups {
		s.mixed[g] = true
	}
}

// result returns the state of each group.  Groups with no values are null
// and string states are null unless keepStrings is true.
func (s *slowMath) result(sctx *super.Context, keepStrings bool) vector.Any {
	b := vector.NewDynamicValueBuilder()
	for g, val := range s.vals {
		switch {
		case s.mixed[g]:
			b.Write(sctx.NewErrorf("mixture of string and numeric values"))
		case !s.set[g] || (!keepStrings && val.Type().ID() == super.IDString):
			b.Write(super.Null)
		default:
			b.Write(val)
		}
	}
	return b.Build(sctx)
}

// toStateType converts numeric val to the state type typ.
func toStateType(val super.Value, typ super.Type) super.Value {
	if val.Type() == typ {
		return val
	}
	switch id := typ.ID(); {
	case super.IsFloat(id):
		f, _ := coerce.ToFloat(val, typ)
		return super.NewFloat(typ, f)
	case super.IsSigned(id):
		i, _ := coerce.ToInt(val, typ)
		return super.NewInt(typ, i)
	default:
		u, _ := coerce.ToUint(val, typ)
		return super.NewUint(typ, u)
	}
}

func sumValues(a, b super.Value) super.Value {
	switch id := a.Type().ID(); {
	case super.IsFloat(id):
		return super.NewFloat(a.Type(), a.Float()+b.Float())
	case super.IsSigned(id):
		return super.NewInt(a.Type(), a.Int()+b.Int())
	case super.IsUnsigned(id):
		return super.NewUint(a.Type(), a.Uint()+b.Uint())
	default:
		// Strings don't sum.
		return a
	}
}

// minMaxValues returns a function that keeps b if it compares to a as sign
// (-1 for min, 1 for max) and a otherwise.
func minMaxValues(sign int) func(a, b super.Value) super.Value {
	return func(a, b super.Value) super.Value {
		var c int
		switch id := a.Type().ID(); {
		case super.IsFloat(id):
			c = cmp.Compare(b.Float(), a.Float())
		case super.IsSigned(id):
			c = cmp.Compare(b.Int(), a.Int())
		case super.IsUnsigned(id):
			c = cmp.Compare(b.Uint(), a.Uint())
		default:
			c = bytes.Compare(b.Bytes(), a.Bytes())
		}
		if c == sign {
			return b
		}
		return a
	}
}
