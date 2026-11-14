package bitvec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBitsGetBits(t *testing.T) {
	assert.Equal(t, []uint64{0x0f}, New([]uint64{0xff}, 4).GetBits())
}

func TestBitsTrueCount(t *testing.T) {
	assert.EqualValues(t, 0, Zero.TrueCount())
	bits := []uint64{0xff}
	assert.EqualValues(t, 1, New(bits, 1).TrueCount())
	assert.EqualValues(t, 8, New(bits, 64).TrueCount())
	bits = []uint64{0xff, 0xff << 56}
	assert.EqualValues(t, 8, New(bits, 65).TrueCount())
	assert.EqualValues(t, 15, New(bits, 127).TrueCount())
	assert.EqualValues(t, 16, New(bits, 128).TrueCount())
}

func TestExtraWords(t *testing.T) {
	// Storage with more words than needed (e.g., after Shorten or from a
	// file written that way) must not affect results.
	a := NewTrue(200)
	a.Shorten(70)
	assert.Equal(t, uint32(70), a.TrueCount())
	assert.Len(t, a.GetBits(), 2)
	assert.Equal(t, uint32(0), Not(a).TrueCount())
	assert.Equal(t, uint32(70), Or(a, NewFalse(70)).TrueCount())
	assert.Equal(t, uint32(0), And(a, NewFalse(70)).TrueCount())
	b := New(make([]uint64, 10), 3)
	assert.Equal(t, uint32(3), Not(b).TrueCount())
	// Append doesn't allocate an extra word at a multiple of 64 bits.
	var c Bits
	for range 64 {
		c.Append(true)
	}
	assert.Len(t, c.bits, 1)
}
