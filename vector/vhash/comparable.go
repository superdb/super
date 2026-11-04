package vhash

import (
	"hash/maphash"
	"net/netip"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

// comparableKey stores fixed-width values of a Go comparable type (UUIDs,
// IP addresses, and networks) and hashes them with maphash.Comparable.
type comparableKey[E comparable] struct {
	vals     []E
	valuesOf func(vector.Any) []E
	build    func([]E) vector.Any

	// Bound input.
	in      []E
	index   []uint32
	scratch []uint32
	baseH   []uint64
}

func newUUIDKey() *comparableKey[uuid.UUID] {
	return &comparableKey[uuid.UUID]{
		valuesOf: func(vec vector.Any) []uuid.UUID { return vec.(*vector.UUID).Values },
		build:    func(vals []uuid.UUID) vector.Any { return vector.NewUUID(vals) },
	}
}

func newIPKey() *comparableKey[netip.Addr] {
	return &comparableKey[netip.Addr]{
		valuesOf: func(vec vector.Any) []netip.Addr { return vec.(*vector.IP).Values },
		build:    func(vals []netip.Addr) vector.Any { return vector.NewIP(vals) },
	}
}

func newNetKey() *comparableKey[netip.Prefix] {
	return &comparableKey[netip.Prefix]{
		valuesOf: func(vec vector.Any) []netip.Prefix { return vec.(*vector.Net).Values },
		build:    func(vals []netip.Prefix) vector.Any { return vector.NewNet(vals) },
	}
}

func (k *comparableKey[E]) bind(vec vector.Any) {
	var base vector.Any
	base, k.index, k.scratch = resolveVec(vec, k.scratch)
	k.in = k.valuesOf(base)
}

func (k *comparableKey[E]) at(row uint32) E {
	if k.index != nil {
		row = k.index[row]
	}
	return k.in[row]
}

func (k *comparableKey[E]) hash(hashes []uint64) {
	switch {
	case k.hashBase():
		for i, slot := range k.index {
			hashes[i] = k.baseH[slot]
		}
	case k.index == nil:
		for i, v := range k.in {
			hashes[i] = maphash.Comparable(seed, v)
		}
	default:
		for i, slot := range k.index {
			hashes[i] = maphash.Comparable(seed, k.in[slot])
		}
	}
}

func (k *comparableKey[E]) combineHash(hashes []uint64) {
	switch {
	case k.hashBase():
		for i, slot := range k.index {
			hashes[i] = combine(hashes[i], k.baseH[slot])
		}
	case k.index == nil:
		for i, v := range k.in {
			hashes[i] = combine(hashes[i], maphash.Comparable(seed, v))
		}
	default:
		for i, slot := range k.index {
			hashes[i] = combine(hashes[i], maphash.Comparable(seed, k.in[slot]))
		}
	}
}

func (k *comparableKey[E]) hashBase() bool {
	if k.index == nil || len(k.in) >= len(k.index) {
		return false
	}
	k.baseH = resize(k.baseH, len(k.in))
	for i, v := range k.in {
		k.baseH[i] = maphash.Comparable(seed, v)
	}
	return true
}

func (k *comparableKey[E]) equal(rows, keyIDs []uint32, eq []bool) {
	for i, row := range rows {
		eq[i] = eq[i] && k.at(row) == k.vals[keyIDs[i]]
	}
}

func (k *comparableKey[E]) append(rows []uint32) {
	for _, row := range rows {
		k.vals = append(k.vals, k.at(row))
	}
}

func (k *comparableKey[E]) vector(*super.Context) vector.Any {
	return k.build(k.vals)
}
