package bsup

import (
	"io"
)

type RawReader struct {
	reader io.Reader
}

func NewRawReader(r io.Reader) *RawReader {
	return &RawReader{reader: r}
}

type RawFrame interface {
	rawFrameType()
}

func (r *RawReader) Next() (RawFrame, error) {
	readerAt, err := scanNextFrameBuffer(r.reader)
	if readerAt == nil || err != nil {
		return nil, err
	}
	h, err := readNextHeader(readerAt, 0)
	if h == nil || err != nil {
		return nil, err
	}
	switch h := h.(type) {
	case *SuperFooter:
		return newRawSuperFrame(readerAt, h), nil
	case *ColumnHeader:
		return newRawColFrame(readerAt, h), nil
	case *RowHeader:
		return newRawRowFrame(readerAt, h), nil
	default:
		panic(h)
	}
}

func (*RawColFrame) rawFrameType()    {}
func (*RawRowFrame) rawFrameType()    {}
func (*RawSuperFooter) rawFrameType() {}

type RawColFrame struct {
	readerAt io.ReaderAt
	header   *ColumnHeader
}

func newRawColFrame(r io.ReaderAt, header *ColumnHeader) *RawColFrame {
	return &RawColFrame{
		readerAt: r,
		header:   header,
	}
}

func (r *RawColFrame) Header() *ColumnHeader {
	return r.header
}

func (r *RawColFrame) VectorTypedefs() ([]byte, error) {
	off, size := r.header.VectorTypedefsSection()
	return io.ReadAll(io.NewSectionReader(r.readerAt, off, size))
}

func (r *RawColFrame) Locators() ([]byte, error) {
	off, size := r.header.LocatorsSection()
	return io.ReadAll(io.NewSectionReader(r.readerAt, off, size))
}

func (r *RawColFrame) Typedefs() ([]byte, error) {
	off, size := r.header.TypedefsSection()
	return io.ReadAll(io.NewSectionReader(r.readerAt, off, size))
}

func (c *RawColFrame) Size() uint64 {
	return c.header.FrameSize
}

type RawRowFrame struct {
	readerAt io.ReaderAt
	header   *RowHeader
}

func newRawRowFrame(r io.ReaderAt, header *RowHeader) *RawRowFrame {
	return &RawRowFrame{
		readerAt: r,
		header:   header,
	}
}

func (r *RawRowFrame) Header() *RowHeader {
	return r.header
}

func (r *RawRowFrame) Size() uint64 {
	return r.header.FrameSize
}

func (r *RawRowFrame) Typedefs() ([]byte, error) {
	off, size := r.header.TypedefsSection()
	return io.ReadAll(io.NewSectionReader(r.readerAt, off, size))
}

type RawSuperFooter struct {
	readerAt io.ReaderAt
	footer   *SuperFooter
}

func newRawSuperFrame(r io.ReaderAt, footer *SuperFooter) *RawSuperFooter {
	return &RawSuperFooter{
		readerAt: r,
		footer:   footer,
	}
}

func (r *RawSuperFooter) Footer() *SuperFooter {
	return r.footer
}

func (r *RawSuperFooter) Size() uint64 {
	return r.footer.Size()
}

func (r *RawSuperFooter) FusedTypeBytes() ([]byte, error) {
	off, size := r.footer.FusedTypeSection()
	return io.ReadAll(io.NewSectionReader(r.readerAt, off, size))
}

func (r *RawSuperFooter) StatsBytes() ([]byte, error) {
	off, size := r.footer.StatsSection()
	return io.ReadAll(io.NewSectionReader(r.readerAt, off, size))
}
