package op

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

// dropPlainNones returns vec without its plain none values or nil if all of
// its values are plain nones.  A plain none comes from an expression like
// ok() or ?. that handles a none.  It disappears from a record, array, or
// map and, as a value output by the values operator or the query, is
// dropped.  A none of a named type and an option's none are values and
// are kept.
func dropPlainNones(vec vector.Any) vector.Any {
	nones := plainNoneSlots(vec)
	if len(nones) == 0 {
		return vec
	}
	n := vec.Len()
	if len(nones) == int(n) {
		return nil
	}
	rest := make([]uint32, 0, int(n)-len(nones))
	var k int
	for slot := range n {
		if k < len(nones) && nones[k] == slot {
			k++
			continue
		}
		rest = append(rest, slot)
	}
	return vector.Pick(vec, rest)
}

// plainNoneSlots returns the slots of vec, in order, whose values are plain
// nones.
func plainNoneSlots(vec vector.Any) []uint32 {
	switch v := vec.(type) {
	case *vector.Dynamic:
		// The values of a dynamic may themselves be dynamics.
		marks := make([][]bool, len(v.Values))
		var found bool
		for k, val := range v.Values {
			if val == nil {
				continue
			}
			if slots := plainNoneSlots(val); len(slots) != 0 {
				marks[k] = markSlots(val.Len(), slots)
				found = true
			}
		}
		if !found {
			return nil
		}
		var nones []uint32
		counts := make([]uint32, len(v.Values))
		for slot, tag := range v.Tags {
			if m := marks[tag]; m != nil && m[counts[tag]] {
				nones = append(nones, uint32(slot))
			}
			counts[tag]++
		}
		return nones
	case *vector.Union:
		tags := v.Tags()
		values := v.Values()
		var nones []uint32
		for slot, tag := range tags {
			if isNoneVec(values[tag]) {
				nones = append(nones, uint32(slot))
			}
		}
		return nones
	case *vector.Fusion:
		// A fusion value is a plain none if its subtype is none.
		var nones []uint32
		for slot, typ := range v.Subtypes.Types() {
			if typ == super.TypeNone {
				nones = append(nones, uint32(slot))
			}
		}
		return nones
	case *vector.View:
		slots := plainNoneSlots(v.Any)
		if len(slots) == 0 {
			return nil
		}
		marks := markSlots(v.Any.Len(), slots)
		var nones []uint32
		for slot, index := range v.Index {
			if marks[index] {
				nones = append(nones, uint32(slot))
			}
		}
		return nones
	}
	if isNoneVec(vec) {
		return allSlots(vec.Len())
	}
	return nil
}

func allSlots(n uint32) []uint32 {
	slots := make([]uint32, n)
	for k := range slots {
		slots[k] = uint32(k)
	}
	return slots
}

func markSlots(n uint32, slots []uint32) []bool {
	marks := make([]bool, n)
	for _, slot := range slots {
		marks[slot] = true
	}
	return marks
}

// isNoneVec reports whether vec is a plain none.  (A none of a named type
// is a value.)
func isNoneVec(vec vector.Any) bool {
	return vec != nil && vec.Type() == super.TypeNone
}
