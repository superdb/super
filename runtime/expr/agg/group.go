package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

// flatten returns the flat vector underlying vec (which must not be named)
// and an index mapping each slot of vec to a slot of the flat vector, or a
// nil index when the mapping is the identity.
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

// orZeros returns groups or, when groups is nil (meaning every row belongs
// to group 0), a slice of n zeros.
func orZeros(groups []uint32, n uint32) []uint32 {
	if groups != nil {
		return groups
	}
	return make([]uint32, n)
}

// grow returns s extended with zero values to length n.  It leaves s
// unchanged when it is nil so lazily allocated state stays unallocated.
func grow[E any](s []E, n uint32) []E {
	if s == nil || uint32(len(s)) >= n {
		return s
	}
	return append(s, make([]E, n-uint32(len(s)))...)
}

// buildByTag builds a result vector with one slot per group where tags[g],
// which must be less than ntags, selects how group g is built.  build is
// called once per tag in use with the groups having that tag in ascending
// order, or with nil when every group has that tag.
func buildByTag[T ~uint8 | ~uint32](tags []T, ntags int, build func(tag T, groups []uint32) vector.Any) vector.Any {
	counts := make([]uint32, ntags)
	var used int
	for _, tag := range tags {
		if counts[tag] == 0 {
			used++
		}
		counts[tag]++
	}
	if used <= 1 {
		var tag T
		if len(tags) > 0 {
			tag = tags[0]
		}
		return build(tag, nil)
	}
	dense := make([]uint32, ntags)
	index := make([][]uint32, 0, used)
	for tag, n := range counts {
		if n > 0 {
			dense[tag] = uint32(len(index))
			index = append(index, make([]uint32, 0, n))
		}
	}
	dynTags := make([]uint32, len(tags))
	for g, tag := range tags {
		d := dense[tag]
		dynTags[g] = d
		index[d] = append(index[d], uint32(g))
	}
	vals := make([]vector.Any, 0, used)
	for tag, n := range counts {
		if n > 0 {
			vals = append(vals, build(T(tag), index[dense[tag]]))
		}
	}
	return vector.NewDynamic(dynTags, vals)
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

// groupArrays returns an array vector of length n whose slot g holds, in
// order, the values of vals whose group is g.  The element type of each
// group's array is the type of its values (a union when they are mixed), as
// if each group were built separately.  Groups with no values get an empty
// array of none or, if nullIfEmpty, a null.
func groupArrays(sctx *super.Context, n uint32, groups []uint32, vals vector.Any, nullIfEmpty bool) vector.Any {
	// Stable counting sort of the values by group.
	offsets := make([]uint32, n+1)
	for _, g := range groups {
		offsets[g+1]++
	}
	for g := range n {
		offsets[g+1] += offsets[g]
	}
	pos := append([]uint32(nil), offsets[:n]...)
	perm := make([]uint32, len(groups))
	for row, g := range groups {
		perm[pos[g]] = uint32(row)
		pos[g]++
	}
	// Each group's element type is determined by the set of value types
	// (Dynamic tags) it holds, so bucket the groups by that set.
	var dynTags, forward []uint32
	var values []vector.Any
	if d, ok := vals.(*vector.Dynamic); ok {
		dynTags, forward, values = d.Tags, d.ForwardTagMap(), d.Values
	} else {
		values = []vector.Any{vals}
	}
	type bucket struct {
		tags    []uint32 // dynamic tags present, ascending
		groups  []uint32
		rows    []uint32 // rows of vals, by group
		offsets []uint32
	}
	var buckets []*bucket
	bucketOf := map[string]int{}
	groupBucket := make([]uint32, n)
	present := make([]bool, len(values))
	var sig []byte
	for g := range n {
		rows := perm[offsets[g]:offsets[g+1]]
		if len(rows) == 0 {
			groupBucket[g] = 0 // empty bucket
			continue
		}
		clear(present)
		for _, row := range rows {
			if dynTags != nil {
				present[dynTags[row]] = true
			} else {
				present[0] = true
			}
		}
		sig = sig[:0]
		var tags []uint32
		for tag, ok := range present {
			if ok {
				sig = append(sig, byte(tag), byte(tag>>8), byte(tag>>16), byte(tag>>24))
				tags = append(tags, uint32(tag))
			}
		}
		id, ok := bucketOf[string(sig)]
		if !ok {
			id = len(buckets)
			bucketOf[string(sig)] = id
			buckets = append(buckets, &bucket{tags: tags, offsets: []uint32{0}})
		}
		b := buckets[id]
		b.groups = append(b.groups, g)
		b.rows = append(b.rows, rows...)
		b.offsets = append(b.offsets, uint32(len(b.rows)))
		groupBucket[g] = uint32(id + 1)
	}
	return buildByTag(groupBucket, len(buckets)+1, func(tag uint32, groups []uint32) vector.Any {
		if tag == 0 {
			// Groups with no values.
			count := n
			if groups != nil {
				count = uint32(len(groups))
			}
			if nullIfEmpty {
				return vector.NewNull(count)
			}
			atyp := sctx.LookupTypeArray(super.TypeNone)
			return vector.NewArray(atyp, make([]uint32, count+1), vector.NewNone(0))
		}
		b := buckets[tag-1]
		var elems vector.Any
		if len(b.tags) == 1 {
			tag := b.tags[0]
			index := b.rows
			if forward != nil {
				index = make([]uint32, len(b.rows))
				for i, row := range b.rows {
					index[i] = forward[row]
				}
			}
			elems = vector.Pick(values[tag], index)
		} else {
			local := make([]uint32, len(values))
			indexes := make([][]uint32, len(b.tags))
			for i, tag := range b.tags {
				local[tag] = uint32(i)
			}
			localTags := make([]uint32, len(b.rows))
			for i, row := range b.rows {
				t := local[dynTags[row]]
				localTags[i] = t
				indexes[t] = append(indexes[t], forward[row])
			}
			vecs := make([]vector.Any, len(b.tags))
			for i, tag := range b.tags {
				vecs[i] = vector.Pick(values[tag], indexes[i])
			}
			elems = vector.NewUnionFromDynamic(sctx, vector.NewDynamic(localTags, vecs))
		}
		return vector.NewArray(sctx.LookupTypeArray(elems.Type()), b.offsets, elems)
	})
}
