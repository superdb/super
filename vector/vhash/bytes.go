package vhash

import (
	"bytes"
	"hash/maphash"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type bytesKey struct {
	typ  super.Type
	vals vector.BytesTable

	// Bound input.
	in      vector.BytesTable
	index   []uint32
	scratch []uint32
	baseH   []uint64
}

func newBytesKey(typ super.Type) *bytesKey {
	return &bytesKey{typ: typ, vals: vector.NewBytesTableEmpty(0)}
}

func (k *bytesKey) bind(vec vector.Any) {
	var base vector.Any
	base, k.index, k.scratch = resolveVec(vec, k.scratch)
	switch base := base.(type) {
	case *vector.String:
		k.in = base.Table()
	case *vector.Bytes:
		k.in = base.Table()
	default:
		panic(base)
	}
}

func (k *bytesKey) hash(hashes []uint64) {
	switch {
	case k.hashBase():
		for i, slot := range k.index {
			hashes[i] = k.baseH[slot]
		}
	case k.index == nil:
		for i := range k.in.Len() {
			hashes[i] = maphash.Bytes(seed, k.in.Bytes(i))
		}
	default:
		for i, slot := range k.index {
			hashes[i] = maphash.Bytes(seed, k.in.Bytes(slot))
		}
	}
}

func (k *bytesKey) combineHash(hashes []uint64) {
	switch {
	case k.hashBase():
		for i, slot := range k.index {
			hashes[i] = combine(hashes[i], k.baseH[slot])
		}
	case k.index == nil:
		for i := range k.in.Len() {
			hashes[i] = combine(hashes[i], maphash.Bytes(seed, k.in.Bytes(i)))
		}
	default:
		for i, slot := range k.index {
			hashes[i] = combine(hashes[i], maphash.Bytes(seed, k.in.Bytes(slot)))
		}
	}
}

func (k *bytesKey) hashBase() bool {
	n := k.in.Len()
	if k.index == nil || int(n) >= len(k.index) {
		return false
	}
	k.baseH = resize(k.baseH, int(n))
	for i := range n {
		k.baseH[i] = maphash.Bytes(seed, k.in.Bytes(i))
	}
	return true
}

func (k *bytesKey) equal(rows, keyIDs []uint32, eq []bool) {
	for i, row := range rows {
		eq[i] = eq[i] && bytes.Equal(k.at(row), k.vals.Bytes(keyIDs[i]))
	}
}

func (k *bytesKey) append(rows []uint32) {
	for _, row := range rows {
		k.vals.Append(k.at(row))
	}
}

func (k *bytesKey) at(row uint32) []byte {
	if k.index != nil {
		row = k.index[row]
	}
	return k.in.Bytes(row)
}

func (k *bytesKey) vector(*super.Context) vector.Any {
	if k.typ.ID() == super.IDString {
		return vector.NewString(k.vals)
	}
	return vector.NewBytes(k.vals)
}
