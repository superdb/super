package scode

import (
	"encoding/binary"
	"slices"
)

// Builder provides an efficient API for constructing nested BSUP values.
type Builder struct {
	bytes      Bytes
	containers []int // Stack of open containers (as body offsets within bytes).
}

// NewBuilder returns a new Builder.
func NewBuilder() *Builder {
	return &Builder{}
}

// Reset resets the Builder to be empty.
func (b *Builder) Reset() {
	b.bytes = nil
	b.containers = b.containers[:0]
}

// Truncate resets the Builder to be empty, but unlike Reset, it retains the
// storage returned by Bytes.
func (b *Builder) Truncate() {
	b.bytes = b.bytes[:0]
	b.containers = b.containers[:0]
}

// Grow guarantees that at least n bytes can be added to the Builder's
// underlying buffer without another allocation.
func (b *Builder) Grow(n int) {
	b.bytes = slices.Grow(b.bytes, n)
}

// BeginContainer opens a new container.
func (b *Builder) BeginContainer() {
	// Allocate one byte for the container tag.  When EndContainer writes
	// the tag, it will arrange for additional space if required.
	b.bytes = append(b.bytes, 0)
	// Push the offset of the container body onto the stack.
	b.containers = append(b.containers, len(b.bytes))
}

// EndContainer closes the most recently opened container.  It panics if the
// receiver has no open container.
func (b *Builder) EndContainer() {
	// Pop the container body offset off the stack.
	bodyOff := b.containers[len(b.containers)-1]
	b.containers = b.containers[:len(b.containers)-1]
	tag := uint64(len(b.bytes) - bodyOff)
	tagSize := SizeOfUvarint(tag)
	// BeginContainer allocated one byte for the container tag.
	tagOff := bodyOff - 1
	if tagSize > 1 {
		// Need additional space for the tag, so move body over.
		b.bytes = append(b.bytes[:tagOff+tagSize], b.bytes[bodyOff:]...)
	}
	if binary.PutUvarint(b.bytes[tagOff:], tag) != tagSize {
		panic("bad container tag size")
	}
}

// TransformContainer calls transform, passing it the body of the most recently
// opened container and replacing the original body with the return value.  It
// panics if the receiver has no open container.
func (b *Builder) TransformContainer(transform func(Bytes) Bytes) {
	bodyOff := b.containers[len(b.containers)-1]
	body := transform(b.bytes[bodyOff:])
	b.bytes = append(b.bytes[:bodyOff], body...)
}

// Append appends val.
func (b *Builder) Append(val []byte) {
	b.bytes = Append(b.bytes, val)
}

// Bytes returns the constructed value.  It panics if the receiver has an open
// container.
func (b *Builder) Bytes() Bytes {
	if len(b.containers) > 0 {
		panic("open container")
	}
	return b.bytes
}
