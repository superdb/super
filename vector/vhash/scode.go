package vhash

import (
	"hash/maphash"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

// scodeKey is the fallback for types without specialized storage. Each
// value is stored in its tagged, serialized form (so null and empty bodies
// remain distinct) and compared byte-wise.
type scodeKey struct {
	typ  super.Type
	vals vector.BytesTable

	// Bound input, serialized.
	in vector.BytesTable
	b  scode.Builder
}

func newScodeKey(typ super.Type) *scodeKey {
	return &scodeKey{
		typ:  typ,
		vals: vector.NewBytesTableEmpty(0),
		in:   vector.NewBytesTableEmpty(0),
	}
}

func (k *scodeKey) bind(vec vector.Any) {
	k.in.Reset()
	for i := range vec.Len() {
		k.b.Truncate()
		vec.Serialize(&k.b, i)
		k.in.Append(k.b.Bytes())
	}
}

func (k *scodeKey) hash(hashes []uint64) {
	for i := range k.in.Len() {
		hashes[i] = maphash.Bytes(seed, k.in.Bytes(i))
	}
}

func (k *scodeKey) combineHash(hashes []uint64) {
	for i := range k.in.Len() {
		hashes[i] = combine(hashes[i], maphash.Bytes(seed, k.in.Bytes(i)))
	}
}

func (k *scodeKey) equal(rows, keyIDs []uint32, eq []bool) {
	for i, row := range rows {
		if eq[i] && string(k.in.Bytes(row)) != string(k.vals.Bytes(keyIDs[i])) {
			eq[i] = false
		}
	}
}

func (k *scodeKey) append(rows []uint32) {
	for _, row := range rows {
		k.vals.Append(k.in.Bytes(row))
	}
}

func (k *scodeKey) vector(sctx *super.Context) vector.Any {
	b := vector.NewValueBuilder(k.typ)
	for i := range k.vals.Len() {
		b.Write(scode.Bytes(k.vals.Bytes(i)).Body())
	}
	return b.Build(sctx)
}
