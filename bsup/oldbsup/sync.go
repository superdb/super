package oldbsup

import (
	"context"
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/peeker"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/vector/vio"
)

type scannerSync struct {
	ctx      context.Context
	cancel   context.CancelFunc
	progress vio.Progress
	worker   *worker
	parser   parser
	err      error
	eof      bool
}

func newScannerSync(ctx context.Context, sctx *super.Context, r io.Reader, filter sbuf.Pushdown, opts ReaderOpts) (*scannerSync, error) {
	ctx, cancel := context.WithCancel(ctx)
	s := &scannerSync{
		ctx:    ctx,
		cancel: cancel,
		parser: parser{
			peeker:  peeker.NewReader(r, opts.Size, opts.Max),
			types:   NewDecoder(sctx),
			maxSize: opts.Max,
		},
	}
	var bf *expr.BufferFilter
	var f expr.Evaluator
	if filter != nil {
		var err error
		bf, err = filter.BSUPFilter()
		if err != nil {
			return nil, err
		}
		f, err = filter.DataFilter()
		if err != nil {
			return nil, err
		}
	}
	s.worker = newWorker(ctx, &s.progress, bf, f, opts.Validate)
	return s, nil
}

func (s *scannerSync) Pull(done bool) (sbuf.Batch, error) {
	if done {
		s.eof = true
		return nil, nil
	}
	if s.err != nil || s.eof {
		return nil, s.err
	}
again:
	frame, err := s.parser.read()
	if err != nil {
		if err == io.EOF {
			err = nil
		}
		return nil, err
	}
	if frame.zbuf != nil {
		if err := frame.decompress(); err != nil {
			return nil, err
		}
		frame.zbuf.free()
	}
	b, err := s.worker.scanBatch(frame.ubuf, s.parser.types)
	if b == nil && err == nil {
		goto again
	}
	return b, err
}

func (s *scannerSync) Progress() vio.Progress {
	return s.progress.Copy()
}
