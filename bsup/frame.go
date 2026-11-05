package bsup

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// A FrameIter iterates over the Frames of a sequence of one or more SuperFrames.
// There are two forms: a streaming iterator when the input is not seekable
// (like a fifo, pipe, socket, or terminal) or a seekable where there is random
// access to an entity like a file or cloud object. The seekable version can
// efficiently compute a type across all of the frames (by reading backward);
// it traverses each SuperFrame footer by following the backlink to the previous
// SuperFrame and reading only the fused types and not the whole file.  The stream
// version does not compute a type and returns nils for its FusedType method.
type FrameIter interface {
	// The FusedType context is typically differently from the data context
	// to keep type fusion (which produces just a bytes type value) separate
	// from the data.
	FusedType(*super.Context) (super.Type, error)
	Next() (Frame, error)
}

type Frame interface {
	frameType()
}

func (*ColFrame) frameType() {}
func (*RowFrame) frameType() {}

type ColFrame struct {
	typedefs   *TypeDefs
	dataReader io.ReaderAt
	header     *ColumnHeader
	progress   *vio.Progress
	defs       *super.TypeDefs
	locators   []byte
}

// newColFrame reads the vector typedefs and locator sections of a column frame
// but doesn't decode the locators, which a loader decodes into its shadows
// along with the typedefs of the vectors' types (see loader.NewFrameLoader).
func newColFrame(r io.ReaderAt, header *ColumnHeader) (*ColFrame, error) {
	c := &ColFrame{
		typedefs: NewTypeDefs(),
		header:   header,
	}
	// The vector typedefs and locator sections are adjacent so read them
	// together.
	off, typedefsSize := header.VectorTypedefsSection()
	_, locatorsSize := header.LocatorsSection()
	b, err := readSection(r, off, typedefsSize+locatorsSize)
	if err != nil {
		return nil, err
	}
	defs, ok := super.NewTypeDefsFromBytes(b[:typedefsSize])
	if !ok {
		return nil, errors.New("malformed BSUP vector typedefs section")
	}
	c.defs = defs
	c.locators = b[typedefsSize:]
	off, size := header.TypedefsSection()
	c.typedefs.reader = io.NewSectionReader(r, off, size)
	c.typedefs.subtypesSize = size
	off, size = header.SegmentsSection()
	c.dataReader = io.NewSectionReader(r, off, size)
	return c, nil
}

func readSection(r io.ReaderAt, off, size int64) ([]byte, error) {
	b := make([]byte, size)
	if n, err := r.ReadAt(b, off); err != nil && !(err == io.EOF && n == len(b)) {
		return nil, fmt.Errorf("reading BSUP column frame: %w", err)
	}
	return b, nil
}

// TypeDefs returns the frame's typedefs table for the types of type values
// (as opposed to its vector typedefs table; see VectorTypeDefs).
func (c *ColFrame) TypeDefs() *TypeDefs {
	return c.typedefs
}

// VectorTypeDefs returns the frame's vector typedefs table, whose typedefs
// are decoded by a loader as needed (see ReadTypeDef).
func (c *ColFrame) VectorTypeDefs() *super.TypeDefs {
	return c.defs
}

// Locators returns a reader of the frame's locator section, which checks that
// each segment it decodes is within the frame's data section.
func (c *ColFrame) Locators() *LocatorReader {
	_, size := c.header.SegmentsSection()
	return NewLocatorReader(c.locators, uint64(size))
}

func (c *ColFrame) Size() uint64 {
	return c.header.FrameSize
}

// MetaSize returns the size of the frame's vector typedefs and locators.
func (c *ColFrame) MetaSize() uint64 {
	return c.header.VectorTypedefsSize + c.header.LocatorsSize
}

func (c *ColFrame) TypeSize() uint64 {
	return c.header.TypedefsSize
}

func (c *ColFrame) LinkProgress(p *vio.Progress) {
	c.progress = p
	r := c.typedefs.reader
	if r != nil {
		c.typedefs.reader = &typesReader{r, p}
	}
}

type typesReader struct {
	r        io.Reader
	progress *vio.Progress
}

func (t *typesReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if err != nil {
		return n, err
	}
	atomic.AddInt64(&t.progress.TypesBytesLoaded, int64(n))
	return n, nil
}

func (c *ColFrame) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.dataReader.ReadAt(p, off)
	if err != nil {
		return n, err
	}
	if c.progress != nil {
		atomic.AddInt64(&c.progress.DataBytesLoaded, int64(n))
	}
	return n, nil
}

type RowFrame struct {
	sctx     *super.Context
	readerAt io.ReaderAt
	header   *RowHeader
}

func newRowFrame(sctx *super.Context, r io.ReaderAt, header *RowHeader) (*RowFrame, error) {
	return &RowFrame{
		sctx:     sctx,
		readerAt: r,
		header:   header,
	}, nil
}

func (r *RowFrame) Size() uint64 {
	return r.header.FrameSize
}

func (r *RowFrame) TypeSize() uint64 {
	return r.header.TypedefsSize
}

func (r *RowFrame) Deserialize() (vector.Any, error) {
	// We keep things simple and decode the entire frame into one
	// vector that is returned here.  Each typedefs is independent of
	// other frames and we merely need to map the local typedefs
	// to query sctx.
	off, size := r.header.TypedefsSection()
	typedefBytes := make([]byte, size)
	if err := readHeaderBytes(r.readerAt, off, typedefBytes, "row typedefs section"); err != nil {
		return nil, err
	}
	typedefs, ok := super.NewTypeDefsFromBytes(typedefBytes)
	if !ok {
		return nil, errors.New("corrupt row typedefs")
	}
	mapper := super.NewTypeDefsMapper(r.sctx, typedefs)
	off, size = r.header.DataSection()
	buf := make([]byte, size)
	if err := readHeaderBytes(r.readerAt, off, buf, "row typedefs section"); err != nil {
		return nil, err
	}
	builder := vector.NewDynamicValueBuilder()
	for len(buf) > 0 {
		id, n := binary.Uvarint(buf)
		if n <= 0 {
			return nil, errors.New("corrupt row data")
		}
		buf = buf[n:]
		bytesLen, n := binary.Uvarint(buf)
		if n <= 0 {
			return nil, errors.New("corrupt row data")
		}
		buf = buf[n:]
		if bytesLen > uint64(len(buf)) {
			return nil, errors.New("corrupt row data")
		}
		typ := mapper.LookupType(uint32(id))
		if typ == nil {
			return nil, fmt.Errorf("type ID %d not in typedefs table", id)
		}
		builder.Write(super.NewValue(typ, buf[:bytesLen]))
		buf = buf[bytesLen:]
	}
	vec := builder.Build(r.sctx)
	if r.header.Control {
		vec = &vector.Control{Any: vec}
	}
	return vec, nil
}

func (r *RowFrame) DeserializeValues() ([]super.Value, error) {
	off, size := r.header.TypedefsSection()
	typedefBytes := make([]byte, size)
	if err := readHeaderBytes(r.readerAt, off, typedefBytes, "row typedefs section"); err != nil {
		return nil, err
	}
	typedefs, ok := super.NewTypeDefsFromBytes(typedefBytes)
	if !ok {
		return nil, errors.New("corrupt row typedefs")
	}
	mapper := super.NewTypeDefsMapper(r.sctx, typedefs)
	off, size = r.header.DataSection()
	buf := make([]byte, size)
	if err := readHeaderBytes(r.readerAt, off, buf, "row typedefs section"); err != nil {
		return nil, err
	}
	var vals []super.Value
	for len(buf) > 0 {
		id, n := binary.Uvarint(buf)
		if n <= 0 {
			return nil, errors.New("corrupt row data")
		}
		buf = buf[n:]
		bytesLen, n := binary.Uvarint(buf)
		if n <= 0 {
			return nil, errors.New("corrupt row data")
		}
		buf = buf[n:]
		if bytesLen > uint64(len(buf)) {
			return nil, errors.New("corrupt row data")
		}
		typ := mapper.LookupType(uint32(id))
		if typ == nil {
			return nil, fmt.Errorf("type ID %d not in typedefs table", id)
		}
		vals = append(vals, super.NewValue(typ, buf[:bytesLen]))
		buf = buf[bytesLen:]
	}
	return vals, nil
}
