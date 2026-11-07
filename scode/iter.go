package scode

import (
	"encoding/binary"
	"fmt"
)

// Iter iterates over the sequence of values encoded in Bytes.
type Iter Bytes

// Done returns true if no values remain.
func (i *Iter) Done() bool {
	return len(*i) == 0
}

// Next returns the body of the next value, which is an empty slice for a
// zero-length value.  Next panics if the next value is malformed.
func (i *Iter) Next() Bytes {
	// The tag is the length of the value's body.
	u64, n := binary.Uvarint(*i)
	if n <= 0 {
		panic(fmt.Sprintf("bad uvarint: %d", n))
	}
	end := n + int(u64)
	val := (*i)[n:end]
	*i = (*i)[end:]
	return Bytes(val)
}

// NextTagAndBody returns the next value as a slice containing the value's
// undecoded tag followed by its body.  NextTagAndBody panics if the next
// value is malformed.
func (i *Iter) NextTagAndBody() Bytes {
	u64, n := binary.Uvarint(*i)
	if n <= 0 {
		panic(fmt.Sprintf("bad uvarint: %d", n))
	}
	n += int(u64)
	val := (*i)[:n]
	*i = (*i)[n:]
	return Bytes(val)
}
