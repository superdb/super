package bsup

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/superdb/super"
)

type Stream struct {
	sctx   *super.Context
	reader io.Reader
}

func NewStream(sctx *super.Context, r io.Reader) *Stream {
	return &Stream{sctx: sctx, reader: r}
}

func (s *Stream) Next() (Frame, error) {
	for {
		readerAt, err := s.scanNextFrameBuffer()
		if readerAt == nil || err != nil {
			return nil, err
		}
		h, err := readNextHeader(readerAt, 0)
		if h == nil || err != nil {
			return nil, err
		}
		switch h := h.(type) {
		case *SuperFooter:
			continue
		case *ColumnHeader:
			return newColFrame(readerAt, h)
		case *RowHeader:
			return newRowFrame(s.sctx, readerAt, h)
		default:
			panic(h)
		}
	}
}

func (s *Stream) FusedType(sctx *super.Context) (super.Type, error) {
	return nil, nil
}

func (s *Stream) scanNextFrameBuffer() (io.ReaderAt, error) {
	// All frame types begin with magic(4), version(2), and framesize (8)
	var peek [14]byte
	if _, err := io.ReadFull(s.reader, peek[:]); err != nil {
		if err == io.EOF {
			err = nil
		}
		return nil, err
	}
	size := binary.LittleEndian.Uint64(peek[6:])
	if size > MaxFrameSize {
		return nil, fmt.Errorf("streaming read of BSUP encountered implausible headder size (%d bytes)", size)
	}
	buf := make([]byte, size)
	copy(buf, peek[:])
	// read the rest of the frame into a memory buf that serves as ReaderAt
	if _, err := io.ReadFull(s.reader, buf[14:]); err != nil {
		return nil, err
	}
	return bytes.NewReader(buf), nil
}
