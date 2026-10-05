package bsupio

import (
	"context"
	"errors"
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/runtime/vcache"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

func NewReader(ctx context.Context, sctx *super.Context, r io.Reader, p sbuf.Pushdown, concurrentReaders int) (vio.Scanner, error) {
	if ra, ok := readerAt(r); ok {
		fit := bsup.NewSeekable(sctx, ra)
		return newReader(ctx, sctx, fit, p, concurrentReaders)
	}
	return newStream(sctx, r)
}

func readerAt(r io.Reader) (io.ReaderAt, bool) {
	ra, ok := r.(io.ReaderAt)
	if ok {
		var buf [1]byte
		if _, err := ra.ReadAt(buf[:], 0); err != nil && !errors.Is(err, io.EOF) {
			return nil, false
		}
		return ra, true
	}
	return nil, false
}

type stream struct {
	sctx   *super.Context
	fit    bsup.FrameIter
	reader io.Reader
}

func newStream(sctx *super.Context, r io.Reader) (vio.Scanner, error) {
	return &stream{
		sctx: sctx,
		fit:  bsup.NewStream(sctx, r),
	}, nil
}

func (s *stream) Pull(done bool) (vector.Any, error) {
	frame, err := s.fit.Next()
	if frame == nil || err != nil {
		return nil, err
	}
	switch frame := frame.(type) {
	case *bsup.ColFrame:
		// XXX refactor reader into a puller that is used by both
		vo := vcache.NewReader(frame)
		vec, err := vo.Fetch(s.sctx, nil) //XXX can project here etc (DRY out code with reader)
		if err != nil {
			return nil, err
		}
		if frame.IsControl() {
			vec = &vector.Control{Any: vec}
		}
		return vec, nil
	case *bsup.RowFrame:
		return frame.Deserialize()
	default:
		panic(frame)
	}
}

func (s *stream) Progress() vio.Progress {
	// XXX stub to be filled in on subsequent PR
	return vio.Progress{}
}

func NewValueReader(ctx context.Context, sctx *super.Context, r io.Reader) (sio.Reader, error) {
	reader, err := NewReader(ctx, sctx, r, nil, 1)
	if err != nil {
		return nil, err
	}
	return sbuf.NewReader(reader), nil
}
