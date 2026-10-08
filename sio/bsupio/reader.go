package bsupio

import (
	"context"
	"io"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/bsup/loader"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type reader struct {
	ctx  context.Context
	sctx *super.Context

	activeReaders *atomic.Int64
	ch            chan result
	fit           bsup.FrameIter
	once          sync.Once
	pushdown      sbuf.Pushdown
	metaFilters   []*metafilter
	vecs          [][]vector.Any
	progress      vio.Progress
}

var _ sio.Typer = (*reader)(nil)

func newReader(ctx context.Context, sctx *super.Context, fit bsup.FrameIter, p sbuf.Pushdown, concurrentReaders int) (*reader, error) {
	if concurrentReaders < 1 {
		panic(concurrentReaders)
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
	return &reader{
		ctx:           ctx,
		sctx:          sctx,
		activeReaders: activeReaders,
		fit:           fit,
		pushdown:      p,
		metaFilters:   metaFilters,
		vecs:          make([][]vector.Any, concurrentReaders),
	}, nil
}

type metafilter struct {
	filter     expr.Evaluator
	projection field.Projection
}

func (r *reader) Pull(done bool) (vector.Any, error) {
	return r.ConcurrentPull(done, 0)
}

func (r *reader) ConcurrentPull(done bool, n int) (vector.Any, error) {
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
			if vec != nil {
				n := int64(vec.Len())
				r.progress.Add(vio.Progress{RecordsRead: n, RecordsMatched: n})
			}
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
			size := int64(frame.Size())
			r.progress.Add(vio.Progress{BytesRead: size})
			if len(r.metaFilters) > 0 && pruneObject(r.sctx, r.metaFilters[n], frame) {
				continue
			}
			r.progress.Add(vio.Progress{BytesMatched: size})
			loader := loader.NewFrame(frame)
			var proj field.Projection
			if r.pushdown != nil {
				proj = r.pushdown.Projection()
			}
			if r.pushdown != nil && r.pushdown.Unordered() {
				r.vecs[n], err = loader.FetchUnordered(r.vecs[n][:0], r.sctx, proj)
				if err != nil {
					return nil, err
				}
				if frame.IsControl() {
					panic("control shouldn't happen on this path")
				}
			} else {
				vec, err := loader.Fetch(r.sctx, proj)
				if err != nil {
					return nil, err
				}
				//XXX
				// This is a little fragile but control is only every one value
				// so only every one vector per frame so we don't need to worry
				// about managing control messages across multiple vectors and
				// check for OOB on just this leg.
				// XXX also this is for reading the API and vectors in storage
				// should never have the OOB flag set.  This is layering violating
				// and we should move the OOB bit to a header outside the frame.
				// Not hard and can be fixed size without a length since we can
				// always read forward.  And scan backward can also peek back
				// to see if it needs to skip it.
				if frame.IsControl() {
					vec = &vector.Control{Any: vec}
				}
				r.vecs[n] = append(r.vecs[n], vec)
			}
		case *bsup.RowFrame:
			vec, err := frame.Deserialize()
			if err != nil {
				return nil, err
			}
			size := int64(frame.Size())
			r.progress.Add(vio.Progress{BytesMatched: size, BytesRead: size})
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

func (r *reader) next() (bsup.Frame, error) {
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

func (r *reader) Type() (super.Type, error) {
	return r.fit.FusedType(r.sctx)
}

func (r *reader) Progress() vio.Progress {
	return r.progress
}
