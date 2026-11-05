package bsup

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/superdb/super"
)

// The locator section of a column frame describes where the vectors of the
// frame are in its data section.  It starts with the number of root vectors
// and the type ID of each in the frame's vector typedefs table (more than one
// root vector is a dynamic), followed by the tags segment of a dynamic, and
// then the locators of each root vector.  The locators of a vector are
// encoded in DFS order by traversing its type: each vector starts with its
// encoding kind followed by the locators of the kind, e.g., a plain record is
// the locators of each of its fields in order and a plain int64 is its count
// and its segment.  The offset of each segment is not encoded: since the
// segments are written to the data section in the order they are located,
// each segment's offset is the end of the previous segment.  Lengths that can
// be computed from a vector's children (e.g., a record's length is the length
// of its first field) are not encoded either.  A segment is its compression
// format, its uncompressed length, and, if it's compressed, its length in the
// data section.
//
// See LocatorWriter for the encoding of each type, and the loader package,
// which decodes the locators into a shadow of each vector.

// The encoding kinds of a vector.  A vector of any type may be plain or a
// constant and a vector of a primitive type may also be a dictionary.  A none
// vector holds the values of an option whose values are all none.
const (
	KindPlain = 0
	KindConst = 1
	KindDict  = 2
	KindNone  = 3
)

// MaxLocatorDepth bounds the nesting of vectors in a locator section so a
// malformed section can't exhaust the stack.  The writer won't write a frame
// whose vectors are nested more deeply (see LocatorWriter.Locate).
const MaxLocatorDepth = 10_000

var errMalformedLocators = errors.New("malformed BSUP locator section")

// LocatorWriter encodes the locator section of a column frame.  It tracks the
// offset in the data section of the next segment so it can check that the
// segments are located in the order they are written to the data section.
type LocatorWriter struct {
	bytes []byte
	off   uint64
	depth int
	err   error
}

// Locate encodes the locators of enc, a root vector or a vector nested in the
// vector whose locators are being encoded.  It tracks the nesting depth as the
// loader does so a frame the loader would reject as nested too deeply isn't
// written (see Err).
func (w *LocatorWriter) Locate(enc Encoder) {
	w.depth++
	if w.depth > MaxLocatorDepth && w.err == nil {
		w.err = fmt.Errorf("BSUP vectors nested more than %d deep", MaxLocatorDepth)
	}
	enc.Locate(w)
	w.depth--
}

// Err returns an error if the vectors located are nested too deeply.
func (w *LocatorWriter) Err() error {
	return w.err
}

// Offset returns the offset in the data section of the next segment.
func (w *LocatorWriter) Offset() uint64 {
	return w.off
}

func (w *LocatorWriter) Bytes() []byte {
	return w.bytes
}

func (w *LocatorWriter) WriteKind(kind byte) {
	w.bytes = append(w.bytes, kind)
}

func (w *LocatorWriter) WriteUvarint(v uint64) {
	w.bytes = binary.AppendUvarint(w.bytes, v)
}

func (w *LocatorWriter) WriteCount(n uint32) {
	w.WriteUvarint(uint64(n))
}

// Segment encodes s, which must start at the offset of the next segment.
func (w *LocatorWriter) Segment(s Segment) {
	if s.Offset != w.off {
		panic(fmt.Sprintf("BSUP segment at offset %d located at offset %d", s.Offset, w.off))
	}
	w.bytes = append(w.bytes, s.CompressionFormat)
	w.WriteUvarint(s.MemLength)
	if s.CompressionFormat != CompressionFormatNone {
		w.WriteUvarint(s.Length)
	}
	w.off += s.fileLength()
}

// Value encodes b, distinguishing a nil slice (a null value) from an empty one.
func (w *LocatorWriter) Value(b []byte) {
	if b == nil {
		w.WriteUvarint(0)
		return
	}
	w.WriteUvarint(uint64(len(b)) + 1)
	w.bytes = append(w.bytes, b...)
}

// fileLength returns the number of bytes s occupies in the data section.
// An uncompressed segment is read as MemLength bytes (see Segment.Read), so
// its Length isn't encoded.
func (s Segment) fileLength() uint64 {
	if s.CompressionFormat == CompressionFormatNone {
		return s.MemLength
	}
	return s.Length
}

// LocatorReader decodes the locator section of a column frame, tracking the
// offset in the data section of the next segment.  Its methods panic with an
// error if the section is malformed.  The section isn't validated when a
// column frame is read but as it's decoded, so a loader decoding a malformed
// section panics.
type LocatorReader struct {
	bytes []byte
	off   uint64
	// limit is the size of the data section, which every segment must be
	// within.
	limit uint64
}

// NewLocatorReader returns a reader of the locator section in bytes of a
// frame whose data section is dataSize bytes.
func NewLocatorReader(bytes []byte, dataSize uint64) *LocatorReader {
	return &LocatorReader{bytes: bytes, limit: dataSize}
}

func (r *LocatorReader) Len() int {
	return len(r.bytes)
}

func (r *LocatorReader) ReadKind() byte {
	if len(r.bytes) == 0 {
		panic(errMalformedLocators)
	}
	kind := r.bytes[0]
	r.bytes = r.bytes[1:]
	return kind
}

func (r *LocatorReader) ReadUvarint() uint64 {
	v, n := binary.Uvarint(r.bytes)
	if n <= 0 {
		panic(errMalformedLocators)
	}
	r.bytes = r.bytes[n:]
	return v
}

func (r *LocatorReader) ReadCount() uint32 {
	v := r.ReadUvarint()
	if v > uint64(^uint32(0)) {
		panic(errMalformedLocators)
	}
	return uint32(v)
}

func (r *LocatorReader) TypeID() uint32 {
	return r.ReadCount()
}

func (r *LocatorReader) Segment() Segment {
	s := Segment{Offset: r.off}
	if len(r.bytes) == 0 {
		panic(errMalformedLocators)
	}
	s.CompressionFormat = r.bytes[0]
	r.bytes = r.bytes[1:]
	s.MemLength = r.ReadUvarint()
	switch s.CompressionFormat {
	case CompressionFormatNone:
		s.Length = s.MemLength
	case CompressionFormatLZ4:
		s.Length = r.ReadUvarint()
		// Bound the uncompressed length by LZ4's maximum compression
		// ratio so a malformed segment can't cause a huge allocation.
		if s.MemLength > maxLZ4Ratio*s.Length+maxLZ4Ratio {
			panic(errMalformedLocators)
		}
	default:
		panic(fmt.Errorf("%w: unknown compression format %d", errMalformedLocators, s.CompressionFormat))
	}
	n := s.fileLength()
	if n > r.limit-r.off {
		panic(fmt.Errorf("%w: segment at offset %d of length %d is past the end of the data section", errMalformedLocators, r.off, n))
	}
	r.off += n
	return s
}

// maxLZ4Ratio bounds the ratio of the uncompressed length to the compressed
// length of an LZ4 block.
const maxLZ4Ratio = 256

// AtLimit returns true if the segments decoded so far fill the data section.
func (r *LocatorReader) AtLimit() bool {
	return r.off == r.limit
}

func (r *LocatorReader) Value() []byte {
	n := r.ReadUvarint()
	if n == 0 {
		return nil
	}
	n--
	if n > uint64(len(r.bytes)) {
		panic(errMalformedLocators)
	}
	b := r.bytes[:n:n]
	r.bytes = r.bytes[n:]
	return b
}

// TypeDefReader decodes a typedef of a frame's vector typedefs table into the
// shadow of a vector of the type (see the loader package).  Its methods panic
// with an error if the typedef is malformed.
type TypeDefReader struct {
	id    uint32
	bytes []byte
}

// ReadTypeDef returns the code of the typedef of the complex type id in defs
// and a reader of the rest of the typedef.
func ReadTypeDef(defs *super.TypeDefs, id uint32) (int, *TypeDefReader) {
	b := defs.Serialization(id)
	if len(b) == 0 {
		panic(fmt.Errorf("%w: bad type ID %d", errMalformedLocators, id))
	}
	return int(b[0]), &TypeDefReader{id: id, bytes: b[1:]}
}

func (t *TypeDefReader) bad() {
	panic(fmt.Errorf("%w: bad typedef for type ID %d", errMalformedLocators, t.id))
}

// Length decodes the number of fields of a record, symbols of an enum, or
// members of a union, which must be no more than max.
func (t *TypeDefReader) Length(max int) int {
	n, b := super.DecodeLength(t.bytes)
	if b == nil || n > max {
		t.bad()
	}
	t.bytes = b
	return n
}

// Name decodes the name of a field, symbol, or named type, which references
// the typedef and isn't copied.
func (t *TypeDefReader) Name() []byte {
	n, b := super.DecodeLength(t.bytes)
	if b == nil || n > len(b) {
		t.bad()
	}
	t.bytes = b[n:]
	return b[:n:n]
}

// Child decodes the ID of a child type, which must precede the type (as in
// super.TypeDefsMapper, only a named type may refer to a type that doesn't
// precede it).
func (t *TypeDefReader) Child() uint32 {
	id, b := super.DecodeID(t.bytes)
	if b == nil || id >= t.id {
		t.bad()
	}
	t.bytes = b
	return id
}

// NamedInner decodes the ID of the inner type of a named type.
func (t *TypeDefReader) NamedInner() uint32 {
	id, b := super.DecodeFixedID(t.bytes)
	if b == nil {
		t.bad()
	}
	t.bytes = b
	return id
}

// ReadLocatorRoots decodes the start of a locator section: the type IDs of the
// root vectors and, if there's more than one, the segment of the dynamic tags.
// It panics with an error if the section is malformed.
func ReadLocatorRoots(r *LocatorReader) (ids []uint32, tags Segment) {
	n := r.ReadCount()
	if n == 0 || int(n) > r.Len() {
		panic(fmt.Errorf("%w: bad number of root vectors", errMalformedLocators))
	}
	for range n {
		ids = append(ids, r.TypeID())
	}
	if n > 1 {
		tags = r.Segment()
	}
	return ids, tags
}
