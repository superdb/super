package bsup

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/superdb/super"
)

type Seekable struct {
	sctx     *super.Context
	readerAt io.ReaderAt
	off      int64
}

func NewSeekable(sctx *super.Context, r io.ReaderAt) *Seekable {
	return &Seekable{sctx: sctx, readerAt: r}
}

func (s *Seekable) Next() (Frame, error) {
	for {
		h, err := readNextHeader(s.readerAt, s.off)
		if h == nil || err != nil {
			return nil, err
		}
		switch h := h.(type) {
		case *SuperFooter:
			s.off += int64(h.Size())
		case *ColumnHeader:
			frame, err := newColFrame(io.NewSectionReader(s.readerAt, s.off, int64(h.FrameSize)), h)
			if err != nil {
				return nil, err
			}
			s.off += int64(h.Size())
			return frame, err
		case *RowHeader:
			frame, err := newRowFrame(s.sctx, io.NewSectionReader(s.readerAt, s.off, int64(h.FrameSize)), h)
			s.off += int64(h.Size())
			return frame, err
		default:
			panic(h)
		}
	}
}

func (s *Seekable) FusedType(sctx *super.Context) (super.Type, error) {
	off, err := fileSize(s.readerAt)
	if err != nil {
		return nil, err
	}
	fuser := super.NewFuser(sctx, true)
	for off > 0 {
		footer, err := s.readFooterBackward(sctx, off)
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return nil, err
		}
		// Move off to the start of the footer.
		off -= int64(footer.FooterSize) + SuperFooterPad
		fusedTypeBytesSize := footer.FooterSize - SuperFooterSize
		if fusedTypeBytesSize != 0 {
			fusedTypeBytes := make([]byte, fusedTypeBytesSize)
			if err := readHeaderBytes(s.readerAt, off+SuperFooterSize, fusedTypeBytes, "super footer fused type"); err != nil {
				return nil, err
			}
			typ, err := sctx.LookupByValue(fusedTypeBytes)
			if err != nil {
				return nil, fmt.Errorf("super footer fused type: %w", err)
			}
			fuser.Fuse(typ)
		}
		// Move off to the start of the current SuperFrame, which is also
		// the end of the previous footer.
		off -= int64(footer.SuperFrameSize)
		if off < 0 {
			return nil, fmt.Errorf("super footer back pointer overflow")
		}
	}
	return fuser.Type(), nil
}

func (s *Seekable) readFooterBackward(sctx *super.Context, off int64) (*SuperFooter, error) {
	var sizeBytes [8]byte
	if int(off) < len(sizeBytes) {
		return nil, fmt.Errorf("short super footer: %d bytes", off)
	}
	if err := readHeaderBytes(s.readerAt, off-8, sizeBytes[:], "super footer"); err != nil {
		return nil, err
	}
	footerSize := binary.LittleEndian.Uint64(sizeBytes[:])
	if err := checkSize("super footer", int(footerSize), MaxMetaSize); err != nil {
		return nil, err
	}
	if footerSize == 0 {
		return nil, fmt.Errorf("super footer illegal size 0")

	}
	footerOff := off - int64(footerSize+SuperFooterPad)
	if footerOff < 0 {
		return nil, fmt.Errorf("super footer size %d bytes, larger than buffer %d bytes", footerSize, off)
	}
	// Reader super footer with 4-bytes of magic
	var footerBytes [SuperFooterSize]byte
	if err := readHeaderBytes(s.readerAt, footerOff, footerBytes[:], "super footer"); err != nil {
		return nil, err
	}
	if string(footerBytes[:4]) != SuperMagic {
		return nil, fmt.Errorf("invalid super footer ending at offset %d bytes", off)
	}
	var footer SuperFooter
	footer.Deserialize(footerBytes)
	if footer.FooterSize != footerSize {
		return nil, fmt.Errorf("super footer size mismatch: %d vs %d (ending at offset %d)", footerSize, footer.FooterSize, off)
	}
	return &footer, footer.check()
}

func fileSize(r io.ReaderAt) (int64, error) {
	s, ok := r.(io.Seeker)
	if !ok {
		// XXX in a future PR, we will read and wrap in an in-memory seekable as
		// long as we aren't in stream mode (which also implies dynamic typing).
		return 0, errors.New("reading file type requires seekable input")
	}
	size, err := s.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	if _, err := s.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	return size, nil
}
