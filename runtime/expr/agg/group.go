package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

// extend returns s extended with zero values to length n.
func extend[E any](s []E, n uint32) []E {
	if uint32(len(s)) >= n {
		return s
	}
	return append(s, make([]E, n-uint32(len(s)))...)
}

// groupsOrZeros returns n zeros, for consuming every slot into group 0, if
// all is true and groups otherwise.
func groupsOrZeros(groups []uint32, all bool, n uint32) []uint32 {
	if all {
		return make([]uint32, n)
	}
	return groups
}

func combineNulls(out vector.Any, nullTags []uint32, size uint32) vector.Any {
	if out.Len() == size {
		return out
	}
	null := vector.NewNull(uint32(len(nullTags)))
	if null.Len() == size {
		return null
	}
	return vector.Combine(out, nullTags, null)
}

// expandContainer returns the element values of a container (array or set)
// vector along with the group of each element, given the group of each
// container.
func expandContainer(groups []uint32, vec vector.Any) ([]uint32, vector.Any) {
	var offsets []uint32
	var values vector.Any
	switch v := vector.PushView(vec).(type) {
	case *vector.Array:
		offsets, values = v.Offsets, v.Values
	case *vector.Set:
		offsets, values = v.Offsets, v.Values
	default:
		panic("aggregation partial: not a container")
	}
	n := offsets[len(offsets)-1] - offsets[0]
	elemGroups := make([]uint32, 0, n)
	index := make([]uint32, 0, n)
	for i := range len(offsets) - 1 {
		g := groups[i]
		for k := offsets[i]; k < offsets[i+1]; k++ {
			elemGroups = append(elemGroups, g)
			index = append(index, k)
		}
	}
	if uint32(len(index)) != values.Len() || (len(index) > 0 && index[0] != 0) {
		values = vector.Pick(values, index)
	}
	return elemGroups, values
}

// buildArrays returns an array vector of length n whose slot g holds, in
// order, the values of vals whose group is g.  The element type of each
// group's array is the type of its values (a union when they are mixed), as
// if each group were built separately.  Groups with no values get an empty
// array or, if nullIfEmpty, a null.
func buildArrays(sctx *super.Context, n uint32, groups []uint32, vals vector.Any, nullIfEmpty bool) vector.Any {
	// Stable counting sort of the values by group.
	counts := make([]uint32, n)
	for _, g := range groups {
		counts[g]++
	}
	// Empty groups are left out of the arrays so they don't take on the
	// element type of other groups and are combined in at the end.
	var emptyGroups []uint32
	pos := make([]uint32, n)
	offsets := make([]uint32, 1, n+1)
	for g, count := range counts {
		if count == 0 {
			emptyGroups = append(emptyGroups, uint32(g))
			continue
		}
		pos[g] = offsets[len(offsets)-1]
		offsets = append(offsets, pos[g]+count)
	}
	var empty vector.Any
	if nempty := uint32(len(emptyGroups)); nullIfEmpty {
		empty = vector.NewNull(nempty)
	} else {
		atyp := sctx.LookupTypeArray(super.TypeNone)
		empty = vector.NewArray(atyp, make([]uint32, nempty+1), vector.NewNone(0))
	}
	if uint32(len(emptyGroups)) == n {
		return empty
	}
	perm := make([]uint32, len(groups))
	for row, g := range groups {
		perm[pos[g]] = uint32(row)
		pos[g]++
	}
	tags, inners, offs := expr.SplitListByTypes(sctx, offsets, vector.Pick(vals, perm))
	arrays := make([]vector.Any, 0, len(inners))
	for i, inner := range inners {
		arrays = append(arrays, vector.NewArray(sctx.LookupTypeArray(inner.Type()), offs[i], inner))
	}
	out := arrays[0]
	if len(arrays) > 1 {
		out = vector.NewDynamic(tags, arrays)
	}
	if len(emptyGroups) == 0 {
		return out
	}
	return vector.Combine(out, emptyGroups, empty)
}

// numericStateType returns the state type for numeric type typ: int64,
// uint64, or float64 for plain numbers, and typ itself for duration and
// time.
func numericStateType(typ super.Type) super.Type {
	switch id := typ.ID(); {
	case id == super.IDDuration || id == super.IDTimestamp:
		return typ
	case super.IsFloat(id):
		return super.TypeFloat64
	case super.IsSigned(id):
		return super.TypeInt64
	case super.IsUnsigned(id):
		return super.TypeUint64
	}
	return nil
}

// flatten returns the flat vector underlying vec and an index mapping each
// slot of vec to a slot of the flat vector, or a nil index when the mapping
// is the identity.
func flatten(vec vector.Any) (vector.Any, []uint32) {
	switch v := vec.(type) {
	case *vector.View:
		return v.Any, v.Index
	case *vector.Dict:
		index := make([]uint32, len(v.Index))
		for i, slot := range v.Index {
			index[i] = uint32(slot)
		}
		return v.Any, index
	case *vector.Const:
		return v.Any, make([]uint32, v.Len())
	default:
		return vec, nil
	}
}
