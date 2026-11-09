package op

import (
	"slices"

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
	if vec == nil {
		return nil
	}
	nones, all := plainNoneSlots(vec)
	n := vec.Len()
	if all || len(nones) == int(n) {
		return nil
	}
	if len(nones) == 0 {
		return vec
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
// nones, or true if all of its values are plain nones, in which case the
// slots aren't returned.
func plainNoneSlots(vec vector.Any) ([]uint32, bool) {
	switch v := vec.(type) {
	case *vector.Dynamic:
		// The values of a dynamic may themselves be dynamics.
		marks := make([][]bool, len(v.Values))
		allNone := make([]bool, len(v.Values))
		var found bool
		for k, val := range v.Values {
			if val == nil {
				continue
			}
			slots, all := plainNoneSlots(val)
			if all {
				allNone[k] = true
				found = true
			} else if len(slots) != 0 {
				marks[k] = markSlots(val.Len(), slots)
				found = true
			}
		}
		if !found {
			return nil, false
		}
		var nones []uint32
		counts := make([]uint32, len(v.Values))
		for slot, tag := range v.Tags {
			if allNone[tag] || marks[tag] != nil && marks[tag][counts[tag]] {
				nones = append(nones, uint32(slot))
			}
			counts[tag]++
		}
		return nones, false
	case *vector.Union:
		// Check the type first so the tags of a union without a none
		// member aren't loaded.
		if !hasPlainNoneMember(v.Typ) {
			return nil, false
		}
		tags := v.Tags()
		values := v.Values()
		var nones []uint32
		for slot, tag := range tags {
			if isNoneVec(values[tag]) {
				nones = append(nones, uint32(slot))
			}
		}
		return nones, false
	case *vector.Fusion:
		// A fusion value is a plain none if its subtype is none, which is
		// possible only if the fusion's type has none.  Check the type first
		// so the subtypes needn't be loaded.
		if typ, ok := v.Typ.Type.(*super.TypeUnion); !ok || !hasPlainNoneMember(typ) {
			if v.Typ.Type != super.TypeNone {
				return nil, false
			}
		}
		var nones []uint32
		for slot, typ := range v.Subtypes.Types() {
			if typ == super.TypeNone {
				nones = append(nones, uint32(slot))
			}
		}
		return nones, false
	case *vector.View:
		slots, all := plainNoneSlots(v.Any)
		if all {
			return nil, true
		}
		if len(slots) == 0 {
			return nil, false
		}
		marks := markSlots(v.Any.Len(), slots)
		var nones []uint32
		for slot, index := range v.Index {
			if marks[index] {
				nones = append(nones, uint32(slot))
			}
		}
		return nones, false
	}
	return nil, isNoneVec(vec)
}

// hasPlainNoneMember reports whether typ has plain none as a member.
func hasPlainNoneMember(typ *super.TypeUnion) bool {
	return slices.ContainsFunc(typ.Types, func(t super.Type) bool { return t == super.TypeNone })
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
