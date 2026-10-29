package bsup

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
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
	fuser := super.NewFuser(sctx)
	for off > 0 {
		footer, err := s.readFooterBackward(off)
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return nil, err
		}
		// Move off to the start of the footer.
		off -= int64(footer.Size())
		fusedTypeOff, fusedTypeSize := footer.FusedTypeSection()
		if fusedTypeSize != 0 {
			fusedTypeBytes := make([]byte, fusedTypeSize)
			if err := readHeaderBytes(s.readerAt, off+fusedTypeOff, fusedTypeBytes, "super footer fused type"); err != nil {
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
	return fuser.FusedType(), nil
}

func (s *Seekable) readFooterBackward(off int64) (*SuperFooter, error) {
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
	footerOff := off - int64(footerSize)
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

func (s *Seekable) FilterFrames(sctx *super.Context, projection field.Projection, skip func(super.Value) bool) ([]int64, bool, error) {
	off, err := fileSize(s.readerAt)
	if err != nil {
		return nil, false, err
	}
	var frameOffs []int64
	projPaths := projection.Paths()
	for off > 0 {
		footer, err := s.readFooterBackward(off)
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return frameOffs, false, err
		}
		off -= int64(footer.Size())
		statsOff, statsSize := footer.StatsSection()
		statsOff += off
		off -= int64(footer.SuperFrameSize)
		if statsSize == 0 {
			// If statsSize is 0 we have a row file. We cannot do stats filter
			// so return here.
			return nil, false, nil
		}
		statsBytes := make([]byte, statsSize)
		if err := readHeaderBytes(s.readerAt, statsOff, statsBytes, "super footer stats"); err != nil {
			return nil, false, err
		}
		offs, stats, err := DecodeStats(statsBytes)
		if err != nil {
			return nil, false, err
		}
		for i, o := range offs {
			if skip(statsValue(sctx, projPaths, stats[i])) {
				continue
			}
			// Need to adjust offset relative to current offsets
			frameOffs = append(frameOffs, off+int64(o))
		}
		if off < 0 {
			return nil, false, fmt.Errorf("super footer back pointer overflow")
		}
	}
	return frameOffs, true, nil

}

type OffsetsSeekable struct {
	*Seekable
	offs []int64
}

func NewOffsetsSeekable(s *Seekable, offs []int64) *OffsetsSeekable {
	return &OffsetsSeekable{s, offs}
}

func (o *OffsetsSeekable) Next() (Frame, error) {
	if len(o.offs) == 0 {
		return nil, nil
	}
	off := o.offs[0]
	o.offs = o.offs[1:]
	h, err := readNextHeader(o.readerAt, int64(off))
	if h == nil || err != nil {
		return nil, err
	}
	c, ok := h.(*ColumnHeader)
	if !ok {
		panic("invalid stats section")
	}
	return newColFrame(io.NewSectionReader(o.readerAt, off, int64(c.FrameSize)), c)
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
