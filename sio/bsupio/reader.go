package bsupio

import (
	"context"
	"errors"
	"io"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/bsup/rows"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/runtime/vcache"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/vector"
)

type Reader struct {
	ctx  context.Context
	sctx *super.Context

	activeReaders *atomic.Int64
	ch            chan result
	fit           bsup.FrameIter
	once          sync.Once
	pushdown      sbuf.Pushdown
	metaFilters   []*metafilter
	readerAt      io.ReaderAt
	vecs          [][]vector.Any
}

var _ sio.Typer = (*Reader)(nil)

func NewReader(ctx context.Context, sctx *super.Context, r io.Reader, p sbuf.Pushdown, concurrentReaders int) (*Reader, error) {
	if concurrentReaders < 1 {
		panic(concurrentReaders)
	}
	ra, ok := r.(io.ReaderAt)
	if !ok {
		return nil, errors.New("BSUP requires a seekable input")
	}
	var buf [1]byte
	if _, err := ra.ReadAt(buf[:], 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, errors.New("BSUP requires a seekable input")
	}
	var metaFilters []*metafilter
	if p != nil {
		filter, _, err := p.MetaFilter()
		if err != nil {
			return nil, err
		}
		if filter != nil {
			for range concurrentReaders {
				filter, projection, err := p.MetaFilter()
				if err != nil {
					return nil, err
				}
				metaFilters = append(metaFilters, &metafilter{filter, projection})
			}
		}
	}
	activeReaders := new(atomic.Int64)
	activeReaders.Store(int64(concurrentReaders))
	return &Reader{
		ctx:           ctx,
		sctx:          sctx,
		activeReaders: activeReaders,
		fit:           bsup.NewSeekable(sctx, ra),
		pushdown:      p,
		metaFilters:   metaFilters,
		readerAt:      ra,
		vecs:          make([][]vector.Any, concurrentReaders),
	}, nil
}

type metafilter struct {
	filter     expr.Evaluator
	projection field.Projection
}

func (r *Reader) Pull(done bool) (vector.Any, error) {
	return r.ConcurrentPull(done, 0)
}

func (r *Reader) ConcurrentPull(done bool, n int) (vector.Any, error) {
	if done {
		return nil, nil
	}
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	for {
		if k := len(r.vecs[n]); k > 0 {
			// Return these last to first so r.vecs gets resued.
			vec := r.vecs[n][k-1]
			r.vecs[n] = r.vecs[n][:k-1]
			return vec, nil
		}
		frame, err := r.next()
		if frame == nil || err != nil {
			return nil, err
		}
		switch frame := frame.(type) {
		case *bsup.ColFrame:
			// XXX using the query context for the metadata filter unnecessarily
			// pollutes the type context.  We should use the BSUP local context for
			// this filtering but this will require a little compiler refactoring to be
			// able to build runtime expressions that use different type contexts.
			if len(r.metaFilters) > 0 && pruneObject(r.sctx, r.metaFilters[n], frame) {
				continue
			}
			vo := vcache.NewReader(frame)
			var proj field.Projection
			if r.pushdown != nil {
				proj = r.pushdown.Projection()
			}
			if r.pushdown != nil && r.pushdown.Unordered() {
				r.vecs[n], err = vo.FetchUnordered(r.vecs[n][:0], r.sctx, proj)
				if err != nil {
					return nil, err
				}
			} else {
				vec, err := vo.Fetch(r.sctx, proj)
				if err != nil {
					return nil, err
				}
				r.vecs[n] = append(r.vecs[n], vec)
			}
		case *bsup.RowFrame:
			vec, err := frame.Deserialize()
			if err != nil {
				return nil, err
			}
			r.vecs[n] = append(r.vecs[n], vec)
		default:
			panic(frame)
		}

	}
}

type result struct {
	frame bsup.Frame
	err   error
}

func (r *Reader) next() (bsup.Frame, error) {
	r.once.Do(func() {
		r.ch = make(chan result, runtime.GOMAXPROCS(0))
		go func() {
			for {
				frame, err := r.fit.Next()
				select {
				case r.ch <- result{frame, err}:
				case <-r.ctx.Done():
					return
				}
				if err != nil {
					close(r.ch)
					break
				}
			}
		}()
	})
	select {
	case r, ok := <-r.ch:
		if !ok || r.err != nil {
			if r.err == io.EOF {
				return nil, nil
			}
			return nil, r.err
		}
		return r.frame, nil
	case <-r.ctx.Done():
		return nil, r.ctx.Err()
	}
}

func pruneObject(sctx *super.Context, mf *metafilter, o *bsup.ColFrame) bool {
	vals := o.ProjectMetadata(sctx, mf.projection)
	for _, val := range vals {
		if !mf.filter.Eval(val).Equal(super.False) {
			return false
		}
	}
	return true
}

func (r *Reader) Type() (super.Type, error) {
	return r.fit.FusedType(r.sctx)
}

type RowReader struct {
	*rows.Reader
}

func NewRowReader(sctx *super.Context, r io.Reader) *RowReader {
	return &RowReader{rows.NewReader(sctx, r)}
}
