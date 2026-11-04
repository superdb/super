package vhash

import (
	"github.com/superdb/super"
	"github.com/superdb/super/pkg/byteconv"
	"github.com/superdb/super/vector"
)

type numericKey struct {
	vals     []uint64
	valuesOf func(vector.Any) []uint64
	build    func([]uint64) vector.Any

	// Bound input.
	in      []uint64
	index   []uint32
	scratch []uint32
	baseH   []uint64
}

func newIntKey(typ super.Type) *numericKey {
	return &numericKey{
		valuesOf: func(vec vector.Any) []uint64 {
			return byteconv.ReinterpretSlice[uint64](vec.(*vector.Int).Values)
		},
		build: func(vals []uint64) vector.Any {
			return vector.NewInt(typ, byteconv.ReinterpretSlice[int64](vals))
		},
	}
}

func newUintKey(typ super.Type) *numericKey {
	return &numericKey{
		valuesOf: func(vec vector.Any) []uint64 { return vec.(*vector.Uint).Values },
		build:    func(vals []uint64) vector.Any { return vector.NewUint(typ, vals) },
	}
}

func newFloatKey(typ super.Type) *numericKey {
	return &numericKey{
		valuesOf: func(vec vector.Any) []uint64 {
			return byteconv.ReinterpretSlice[uint64](vec.(*vector.Float).Values)
		},
		build: func(vals []uint64) vector.Any {
			return vector.NewFloat(typ, byteconv.ReinterpretSlice[float64](vals))
		},
	}
}

func (k *numericKey) bind(vec vector.Any) {
	var base vector.Any
	base, k.index, k.scratch = resolveVec(vec, k.scratch)
	k.in = k.valuesOf(base)
}

func (k *numericKey) hash(hashes []uint64) {
	switch {
	case k.hashBase():
		for i, slot := range k.index {
			hashes[i] = k.baseH[slot]
		}
	case k.index == nil:
		for i, v := range k.in {
			hashes[i] = fmix64(v)
		}
	default:
		for i, slot := range k.index {
			hashes[i] = fmix64(k.in[slot])
		}
	}
}

func (k *numericKey) combineHash(hashes []uint64) {
	switch {
	case k.hashBase():
		for i, slot := range k.index {
			hashes[i] = combine(hashes[i], k.baseH[slot])
		}
	case k.index == nil:
		for i, v := range k.in {
			hashes[i] = combine(hashes[i], fmix64(v))
		}
	default:
		for i, slot := range k.index {
			hashes[i] = combine(hashes[i], fmix64(k.in[slot]))
		}
	}
}

func (k *numericKey) hashBase() bool {
	if k.index == nil || len(k.in) >= len(k.index) {
		return false
	}
	k.baseH = resize(k.baseH, len(k.in))
	for i, v := range k.in {
		k.baseH[i] = fmix64(v)
	}
	return true
}

func (k *numericKey) equal(rows, keyIDs []uint32, eq []bool) {
	if k.index != nil {
		for i, row := range rows {
			eq[i] = eq[i] && k.in[k.index[row]] == k.vals[keyIDs[i]]
		}
	} else {
		for i, row := range rows {
			eq[i] = eq[i] && k.in[row] == k.vals[keyIDs[i]]
		}
	}
}

func (k *numericKey) append(rows []uint32) {
	for _, row := range rows {
		if k.index != nil {
			row = k.index[row]
		}
		k.vals = append(k.vals, k.in[row])
	}
}

func (k *numericKey) vector(*super.Context) vector.Any {
	return k.build(k.vals)
}
