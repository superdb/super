// Package vhash provides a vectorized hash table that maps tuples of key
// values to dense key IDs.  It is the shared core for operators that need
// to look up keys in bulk, e.g., distinct, hash join, and aggregation.
package vhash

import (
	"fmt"
	"math"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

// Miss is the key ID that Find reports for keys not in the table.
const Miss = math.MaxUint32

const (
	saltMask  = 0xffffffff << 32
	keyIDMask = 1<<32 - 1
	minSlots  = 256
)

type Table struct {
	types  []super.Type
	keys   []key
	slots  []uint64 // salt | (keyID+1); 0 means empty
	mask   uint64
	hashes []uint64 // hash of each stored key, by key ID

	// Per-call scratch.
	h          []uint64
	offsets    []uint64
	keyIDs     []uint32
	created    []uint32
	remaining  []uint32
	newRows    []uint32
	candRows   []uint32
	candKeyIDs []uint32
	eq         []bool
}

// NewTable returns an empty table for keys of the given types.  It panics if
// types is empty.
func NewTable(types []super.Type) *Table {
	if len(types) == 0 {
		panic("vhash.NewTable: no key types")
	}
	t := &Table{types: types}
	for _, typ := range types {
		t.keys = append(t.keys, newKey(typ))
	}
	t.resizeTable(minSlots)
	return t
}

// Len returns the number of distinct keys in the table.
func (t *Table) Len() uint32 {
	return uint32(len(t.hashes))
}

// Types returns the key types of the table.
func (t *Table) Types() []super.Type {
	return t.types
}

// FindOrInsert looks up each row of keys, inserting the keys not already
// present.  keys must have one vector per key type, each of the matching type
// and all of the same length.  It returns the key ID of each row and the
// rows that inserted new keys.  Key IDs are assigned densely in creation
// order, which is not necessarily row order (a row whose probe hits a salt
// collision is resolved in a later pass).  The returned slices are owned by t
// and are valid until the next call.
func (t *Table) FindOrInsert(keys []vector.Any) (keyIDs, created []uint32) {
	return t.lookup(keys, true)
}

// Find looks up each row of keys and returns its key ID, or Miss if the key
// is not in the table.  The returned slice is owned by t and is valid until
// the next call.
func (t *Table) Find(keys []vector.Any) []uint32 {
	keyIDs, _ := t.lookup(keys, false)
	return keyIDs
}

func (t *Table) Keys(sctx *super.Context) []vector.Any {
	var out []vector.Any
	for _, k := range t.keys {
		out = append(out, k.vector(sctx))
	}
	return out
}

func (t *Table) lookup(vecs []vector.Any, insert bool) ([]uint32, []uint32) {
	if len(vecs) != len(t.keys) {
		panic(fmt.Sprintf("vhash: got %d key vectors, table expects %d", len(vecs), len(t.keys)))
	}
	n := int(vecs[0].Len())
	for i, vec := range vecs {
		if int(vec.Len()) != n {
			panic("vhash: key vectors have different lengths")
		}
		if _, ok := vec.(*vector.Dynamic); ok {
			panic(fmt.Sprintf("vhash: key %d is a vector.Dynamic; split it by type first", i))
		}
		if typ := vec.Type(); typ != t.types[i] {
			panic(fmt.Sprintf("vhash: key %d has type %s, table expects %s",
				i, sup.FormatType(typ), sup.FormatType(t.types[i])))
		}
	}
	t.keyIDs = resize(t.keyIDs, n)
	t.created = t.created[:0]
	if n == 0 {
		return t.keyIDs, t.created
	}
	for i, k := range t.keys {
		k.bind(vecs[i])
	}
	t.h = resize(t.h, n)
	hashes := t.h
	t.keys[0].hash(hashes)
	for _, k := range t.keys[1:] {
		k.combineHash(hashes)
	}
	if insert {
		t.reserve(uint64(len(t.hashes)) + uint64(n))
	}
	t.offsets = resize(t.offsets, n)
	for i, h := range hashes {
		t.offsets[i] = h & t.mask
	}
	t.remaining = resize(t.remaining, n)
	for i := range t.remaining {
		t.remaining[i] = uint32(i)
	}
	for len(t.remaining) > 0 {
		t.newRows = t.newRows[:0]
		t.candRows = t.candRows[:0]
		t.candKeyIDs = t.candKeyIDs[:0]
		// Probe each remaining row until it finds an empty slot or a
		// slot with a matching salt.
		for _, row := range t.remaining {
			h := hashes[row]
			salt := h & saltMask
			off := t.offsets[row]
			for {
				e := t.slots[off]
				if e == 0 {
					if insert {
						keyID := uint32(len(t.hashes))
						t.slots[off] = salt | uint64(keyID+1)
						t.hashes = append(t.hashes, h)
						t.keyIDs[row] = keyID
						t.newRows = append(t.newRows, row)
					} else {
						t.keyIDs[row] = Miss
					}
					break
				}
				if e&saltMask == salt {
					t.candRows = append(t.candRows, row)
					t.candKeyIDs = append(t.candKeyIDs, uint32(e&keyIDMask-1))
					break
				}
				// Occupied by another key; try the next slot, wrapping at the
				// end
				off = (off + 1) & t.mask
			}
			t.offsets[row] = off
		}
		// Store the new keys before comparing candidates since
		// a candidate may be a later duplicate of a new key in
		// this same batch.
		if len(t.newRows) > 0 {
			for _, k := range t.keys {
				k.append(t.newRows)
			}
			t.created = append(t.created, t.newRows...)
		}
		// Compare candidates' keys.  Matches are resolved; mismatches
		// (salt collisions) continue probing at the next slot.
		t.remaining = t.remaining[:0]
		if len(t.candRows) > 0 {
			t.eq = resize(t.eq, len(t.candRows))
			for i := range t.eq {
				t.eq[i] = true
			}
			for _, k := range t.keys {
				k.equal(t.candRows, t.candKeyIDs, t.eq)
			}
			for i, row := range t.candRows {
				if t.eq[i] {
					t.keyIDs[row] = t.candKeyIDs[i]
				} else {
					t.offsets[row] = (t.offsets[row] + 1) & t.mask
					t.remaining = append(t.remaining, row)
				}
			}
		}
	}
	return t.keyIDs, t.created
}

// reserve grows the slot array if needed so that it can hold n keys at a
// load factor of at most 3/4.
func (t *Table) reserve(n uint64) {
	// Key IDs are uint32 and Miss is reserved, so there can be at most
	// Miss keys (IDs 0 through Miss-1).
	if n > Miss {
		panic("vhash: too many keys")
	}
	size := uint64(len(t.slots))
	for n*4 > size*3 {
		size *= 2
	}
	if size != uint64(len(t.slots)) {
		t.resizeTable(size)
	}
}

// resize returns s with length n, reusing its capacity when possible.
func resize[E any](s []E, n int) []E {
	return slices.Grow(s[:0], n)[:n]
}

func (t *Table) resizeTable(size uint64) {
	t.slots = make([]uint64, size)
	t.mask = size - 1
	for keyID, h := range t.hashes {
		off := h & t.mask
		for t.slots[off] != 0 {
			off = (off + 1) & t.mask
		}
		t.slots[off] = h&saltMask | uint64(keyID+1)
	}
}
