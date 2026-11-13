package vhash

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/bitvec"
)

func TestTableFindOrInsert(t *testing.T) {
	tab := NewTable([]super.Type{super.TypeInt64})
	keyIDs, created := tab.FindOrInsert([]vector.Any{ints(5, 7, 5, 9, 7, 5)})
	assert.Equal(t, []uint32{0, 1, 0, 2, 1, 0}, keyIDs)
	assert.Equal(t, []uint32{0, 1, 3}, created)

	keyIDs, created = tab.FindOrInsert([]vector.Any{ints(9, 11, 5, 11)})
	assert.Equal(t, []uint32{2, 3, 0, 3}, keyIDs)
	assert.Equal(t, []uint32{1}, created)
	assert.Equal(t, uint32(4), tab.Len())

	keys := tab.Keys(super.NewContext())
	assert.Equal(t, []int64{5, 7, 9, 11}, keys[0].(*vector.Int).Values)

	assert.Equal(t, []uint32{3, Miss, 0}, tab.Find([]vector.Any{ints(11, 12, 5)}))
	assert.Equal(t, uint32(4), tab.Len())
}

// TestTableTypeMismatch checks that keys whose type differs from the
// table's are rejected even when they share a vector representation.
func TestTableTypeMismatch(t *testing.T) {
	tab := NewTable([]super.Type{super.TypeInt64})
	assert.Panics(t, func() {
		tab.FindOrInsert([]vector.Any{vector.NewInt(super.TypeInt32, []int64{1})})
	})
}

// TestTableSaltCollision forces keys that share a home slot and a salt but
// differ, which exercises the mismatch-and-retry path of the probe loop.
func TestTableSaltCollision(t *testing.T) {
	const salt = 0xabcd1234 << 32
	k1 := int64(unmix(salt | 5))         // home slot 5
	k2 := int64(unmix(salt | 1<<16 | 5)) // home slot 5, same salt, different key
	k3 := int64(unmix(0x11111111<<32 | 50))
	require.Equal(t, uint64(salt|5), fmix64(uint64(k1)))

	tab := NewTable([]super.Type{super.TypeInt64})
	keyIDs, created := tab.FindOrInsert([]vector.Any{ints(k1, k2, k3, k1, k2)})
	// k2 collides with k1 in the first pass and is inserted in the second,
	// so it gets a higher key ID than k3 even though it comes first.
	assert.Equal(t, []uint32{0, 2, 1, 0, 2}, keyIDs)
	assert.Equal(t, []uint32{0, 2, 1}, created)
	assert.Equal(t, []uint32{0, 2, 1, Miss}, tab.Find([]vector.Any{ints(k1, k2, k3, int64(unmix(salt|1<<17|5)))}))
}

// unmix inverts fmix64 so tests can construct int64 keys with chosen hashes.
func unmix(h uint64) uint64 {
	// x ^= x >> 33 is its own inverse since 2*33 >= 64.
	h ^= h >> 33
	h *= inverse(0xc4ceb9fe1a85ec53)
	h ^= h >> 33
	h *= inverse(0xff51afd7ed558ccd)
	h ^= h >> 33
	return h
}

// inverse returns the multiplicative inverse of odd a modulo 2^64.
func inverse(a uint64) uint64 {
	x := a // correct to 3 bits; each Newton step doubles that
	for range 5 {
		x *= 2 - a*x
	}
	return x
}

// TestTableResize inserts enough keys across batches to grow the slot array
// several times and checks that every key is still found with its key ID.
func TestTableResize(t *testing.T) {
	tab := NewTable([]super.Type{super.TypeInt64})
	var vals []int64
	var keyIDs []uint32
	for b := range 10 {
		batch := make([]int64, 1000)
		for i := range batch {
			batch[i] = int64(b*1000 + i)
		}
		ids, created := tab.FindOrInsert([]vector.Any{ints(batch...)})
		require.Len(t, created, 1000)
		vals = append(vals, batch...)
		keyIDs = append(keyIDs, ids...)
	}
	require.Equal(t, uint32(len(vals)), tab.Len())
	assert.Equal(t, keyIDs, tab.Find([]vector.Any{ints(vals...)}))
	keys := tab.Keys(super.NewContext())
	for i, keyID := range keyIDs {
		require.Equal(t, vals[i], vector.IntValue(keys[0], keyID))
	}
}

func ints(vals ...int64) vector.Any {
	return vector.NewInt(super.TypeInt64, vals)
}

func strs(vals ...string) vector.Any {
	b := vector.NewStringEmpty(0)
	for _, v := range vals {
		b.Append(v)
	}
	return b
}

func TestTableBoolKeys(t *testing.T) {
	tab := NewTable([]super.Type{super.TypeBool, super.TypeString})
	keyIDs, created := tab.FindOrInsert([]vector.Any{
		bools(true, false, true, true, false),
		strs("a", "a", "a", "b", "a"),
	})
	assert.Equal(t, []uint32{0, 1, 0, 2, 1}, keyIDs)
	assert.Equal(t, []uint32{0, 1, 3}, created)

	// Const and View inputs.
	keyIDs, _ = tab.FindOrInsert([]vector.Any{vector.NewConstBool(false, 2), strs("b", "a")})
	assert.Equal(t, []uint32{3, 1}, keyIDs)
	view := vector.NewView(bools(false, true), []uint32{1, 1, 0})
	assert.Equal(t, []uint32{0, 2, Miss}, tab.Find([]vector.Any{view, strs("a", "b", "c")}))

	keys := tab.Keys(super.NewContext())
	b := keys[0].(*vector.Bool)
	var got []bool
	for i := range b.Len() {
		got = append(got, b.IsSet(i))
	}
	assert.Equal(t, []bool{true, false, true, false}, got)
}

func bools(vals ...bool) vector.Any {
	bits := bitvec.NewFalse(uint32(len(vals)))
	for i, v := range vals {
		bits.SetTo(uint32(i), v)
	}
	return vector.NewBool(bits)
}
