package vhash

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

// noneHash is the hash of every none value.  Any nonzero constant works
// since a key position holds values of a single type.
const noneHash = 0x9e3779b97f4a7c15

// noneKey is a key of type none.  Every none value is equal, so only the
// number of stored keys needs to be tracked.
type noneKey struct {
	n uint32
}

func (*noneKey) bind(vector.Any) {}

func (*noneKey) hash(hashes []uint64) {
	for i := range hashes {
		hashes[i] = noneHash
	}
}

func (*noneKey) combineHash(hashes []uint64) {
	for i, h := range hashes {
		hashes[i] = combine(h, noneHash)
	}
}

func (*noneKey) equal([]uint32, []uint32, []bool) {}

func (k *noneKey) append(rows []uint32) {
	k.n += uint32(len(rows))
}

func (k *noneKey) vector(*super.Context) vector.Any {
	return vector.NewNone(k.n)
}

// nullKey is a key of type null.  Like none, every null value is equal, so it
// is a noneKey that builds a null vector.
type nullKey struct {
	noneKey
}

func (k *nullKey) vector(*super.Context) vector.Any {
	return vector.NewNull(k.n)
}
