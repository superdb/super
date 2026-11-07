// Package scode implements serialization and deserialzation for BSUP values.
//
// A value is represented by an unsigned integer tag followed by a
// byte-sequence body, where the tag is the length of the body.
//
// Values of container type (record, array, set, or map) are represented
// similarly, with the body containing a sequence of zero or more serialized
// values.  Values of union and option type are containers whose first value is
// an integer tag (for a union, the index of the value's type in the union
// type; for an option, 0 for some and 1 for none) followed by the value
// (absent for an option's none).
package scode

import (
	"encoding/binary"
	"errors"
	"io"
)

var ErrNotSingleton = errors.New("value body has more than one encoded value")

// Bytes is the serialized representation of a sequence of BSUP values.
type Bytes []byte

// Iter returns an Iter for the receiver.
func (b Bytes) Iter() Iter {
	return Iter(b)
}

// Body returns b's body.
func (b Bytes) Body() Bytes {
	it := b.Iter()
	return it.Next()
}

// Append appends val to dst as a tagged value and returns the
// extended buffer.
func Append(dst Bytes, val []byte) Bytes {
	dst = binary.AppendUvarint(dst, uint64(len(val)))
	return append(dst, val...)
}

// SizeOfUvarint returns the number of bytes required by binary.AppendUvarint to
// represent u64.
func SizeOfUvarint(u64 uint64) int {
	n := 1
	for u64 >= 0x80 {
		n++
		u64 >>= 7
	}
	return n
}

func ReadTag(r io.ByteReader) (int, error) {
	v, err := binary.ReadUvarint(r)
	return int(v), err
}
