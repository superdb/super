package function

import (
	"bytes"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/bitvec"
)

// Has implements has(val, key), which is true if
//   - val is a record whose type has a field named key,
//   - val is a type value of a record type with a field named key, or
//   - val is a map with the key key.
//
// For a record, the answer depends only on its type, so it's true for a
// field whose value is null, an error, or an option's none.  (A plain none
// never appears in a record.)  It's false for any other value.
type Has struct {
	sctx *super.Context
	mb   scode.Builder
	kb   scode.Builder
}

func (h *Has) Call(args ...vector.Any) vector.Any {
	val, key := args[0], vector.Under(args[1])
	if key.Kind() == vector.KindError {
		return key
	}
	n := val.Len()
	switch typ := super.TypeUnder(val.Type()).(type) {
	case *super.TypeRecord:
		if err := h.checkFieldName(key); err != nil {
			return err
		}
		if c, ok := key.(*vector.Const); ok {
			return vector.NewConstBool(typ.HasField(vector.StringValue(c, 0)), n)
		}
		return h.eachSlot(n, func(slot uint32) bool {
			return typ.HasField(vector.StringValue(key, slot))
		})
	case *super.TypeMap:
		return h.eachSlot(n, func(slot uint32) bool {
			return h.hasMapKey(typ, val, key, slot)
		})
	}
	if val.Kind() == vector.KindType && key.Kind() == vector.KindString {
		// A type value is tested by its record type.  Any other type
		// value, or a key that isn't a string, is false.
		return h.eachSlot(n, func(slot uint32) bool {
			rec, ok := super.TypeUnder(vector.TypeValueValue(val, slot)).(*super.TypeRecord)
			return ok && rec.HasField(vector.StringValue(key, slot))
		})
	}
	return vector.NewConstBool(false, n)
}

func (h *Has) checkFieldName(key vector.Any) vector.Any {
	if key.Kind() != vector.KindString {
		return vector.NewWrappedError(h.sctx, "has: field name must be a string", key)
	}
	return nil
}

func (h *Has) eachSlot(n uint32, fn func(uint32) bool) vector.Any {
	bits := bitvec.NewFalse(n)
	for slot := range n {
		if fn(slot) {
			bits.Set(slot)
		}
	}
	return vector.NewBool(bits)
}

// hasMapKey returns true if the map at slot of vec has the key at slot of
// key.  Keys are compared by value after removing named and union types.
func (h *Has) hasMapKey(typ *super.TypeMap, vec, key vector.Any, slot uint32) bool {
	m := vector.ValueAt(&h.mb, vec, slot)
	if m.IsNull() {
		return false
	}
	body := m.Bytes()
	k := vector.ValueAt(&h.kb, key, slot).Under()
	for it := body.Iter(); !it.Done(); {
		mk := super.NewValue(typ.KeyType, it.Next()).Under()
		it.Next()
		if mk.Type() == k.Type() && bytes.Equal(mk.Bytes(), k.Bytes()) {
			return true
		}
	}
	return false
}
