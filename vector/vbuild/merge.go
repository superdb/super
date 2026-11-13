package vbuild

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

func MergeSameTypesInDynamic(d *vector.Dynamic) vector.Any {
	m := make(map[super.Type][]uint32)
	for i, vec := range d.Values {
		if vec == nil {
			// Filter out nil values in dynamic.
			m[nil] = nil
			continue
		}
		typ := vec.Type()
		m[typ] = append(m[typ], uint32(i))
	}
	if len(m) == len(d.Values) {
		return d
	}
	if len(m) == 1 {
		return Merge(d.Tags, d.Values)
	}
	remapTags := make([]uint32, len(d.Values))
	var newVecs []vector.Any
	for typ, valIdx := range m {
		if typ == nil {
			continue
		}
		if len(valIdx) > 1 {
			vecs := make([]vector.Any, len(valIdx))
			tagMap := slices.Repeat([]int{-1}, len(d.Values))
			for i, tag := range valIdx {
				remapTags[tag] = uint32(len(newVecs))
				tagMap[tag] = i
				vecs[i] = d.Values[tag]
			}
			var tags []uint32
			for _, tag := range d.Tags {
				if newTag := tagMap[tag]; newTag != -1 {
					tags = append(tags, uint32(newTag))
				}
			}
			newVecs = append(newVecs, Merge(tags, vecs))
		} else {
			remapTags[valIdx[0]] = uint32(len(newVecs))
			newVecs = append(newVecs, d.Values[valIdx[0]])
		}
	}
	// remap the dynamic tags
	newTags := make([]uint32, len(d.Tags))
	for i, tag := range d.Tags {
		newTags[i] = remapTags[tag]
	}
	return vector.NewDynamic(newTags, newVecs)
}

// Merge merges the same type vectors vecs into a single vector of the same
// type.
func Merge(tags []uint32, vecs []vector.Any) vector.Any {
	// assert vecs are same type
	typ := vecs[0].Type()
	for _, vec := range vecs {
		if vec.Type() != typ {
			panic("merge on vectors not of same type")
		}
	}
	// Concat vectors together then use a view to maintain original order.
	b := New(typ)
	reverse := make([][]uint32, len(vecs))
	var k uint32
	for i, vec := range vecs {
		b.Write(vec)
		for range vec.Len() {
			reverse[i] = append(reverse[i], k)
			k++
		}
	}
	out := b.Build()
	counts := make([]uint32, len(vecs))
	index := make([]uint32, len(tags))
	for i, tag := range tags {
		index[i] = reverse[tag][counts[tag]]
		counts[tag]++
	}
	return vector.Pick(out, index)
}
