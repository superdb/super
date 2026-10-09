package bsupio

import (
	"context"
	"io"
	"runtime"
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/bsup/loader"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type dispatcher struct {
	ctx  context.Context
	sctx *super.Context

	readers  []reader
	frameCh  chan frame
	fit      bsup.FrameIter
	once     sync.Once
	progress vio.Progress
}

var _ sio.Typer = (*dispatcher)(nil)

func newDispatcher(ctx context.Context, sctx *super.Context, fit bsup.FrameIter, p vio.Pushdown, concurrentReaders int) (*dispatcher, error) {
	if concurrentReaders < 1 {
		panic(concurrentReaders)
	}
	frameCh := make(chan frame, runtime.GOMAXPROCS(0))
	d := &dispatcher{
		ctx:     ctx,
		sctx:    sctx,
		frameCh: frameCh,
		fit:     fit,
	}
	for range concurrentReaders {
		r, err := newReader(ctx, sctx, d, p)
		if err != nil {
			return nil, err
		}
		d.readers = append(d.readers, r)
	}
	return d, nil
}

func (d *dispatcher) Pull(done bool) (vector.Any, error) {
	return d.ConcurrentPull(done, 0)
}

func (d *dispatcher) ConcurrentPull(done bool, id int) (vector.Any, error) {
	return d.readers[id].Pull(done)
}

func (d *dispatcher) Type() (super.Type, error) {
	return d.fit.FusedType(d.sctx)
}

func (d *dispatcher) Progress() vio.Progress {
	return d.progress
}

func (d *dispatcher) next() (bsup.Frame, error) {
	d.once.Do(func() {
		go func() {
			for {
				f, err := d.fit.Next()
				if err == io.EOF {
					err = nil
				}
				select {
				case d.frameCh <- frame{f, err}:
				case <-d.ctx.Done():
					return
				}
				if err != nil {
					close(d.frameCh)
					break
				}
			}
		}()
	})
	select {
	case f := <-d.frameCh:
		return f.frame, f.err
	case <-d.ctx.Done():
		return nil, d.ctx.Err()
	}
}

type reader struct {
	ctx  context.Context
	sctx *super.Context

	dispatcher *dispatcher
	metaFilter *metafilter
	pushdown   vio.Pushdown
	q          []vector.Any
}

func newReader(ctx context.Context, sctx *super.Context, d *dispatcher, pushdown vio.Pushdown) (reader, error) {
	var metaFilter *metafilter
	if pushdown != nil {
		filter, projection, err := pushdown.MetaFilter()
		if err != nil {
			return reader{}, err
		}
		if filter != nil {
			metaFilter = &metafilter{filter, projection}
		}
	}
	return reader{
		ctx:        ctx,
		sctx:       sctx,
		dispatcher: d,
		metaFilter: metaFilter,
		pushdown:   pushdown,
	}, nil
}

type metafilter struct {
	filter     expr.Evaluator
	projection field.Projection
}

func (r *reader) Pull(done bool) (vector.Any, error) {
	if done {
		r.q = r.q[:0]
		return nil, nil
	}
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if k := len(r.q); k > 0 {
		// Order doesn't matter here so peel the vectors off end of q
		// so we can reuse the underlying slice when we pass it back
		// to LoadUnordered for the next set of vectors.
		vec := r.q[k-1]
		r.q = r.q[:k-1]
		n := int64(vec.Len())
		r.dispatcher.progress.Add(vio.Progress{RecordsRead: n, RecordsMatched: n})
		return vec, nil
	}
	for {
		frame, err := r.dispatcher.next()
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
			r.dispatcher.progress.Add(vio.Progress{BytesRead: size})
			if r.metaFilter != nil && skipFrame(r.sctx, r.metaFilter, frame) {
				continue
			}
			r.dispatcher.progress.Add(vio.Progress{BytesMatched: size})
			loader := loader.NewFrameLoader(frame)
			var proj field.Projection
			if r.pushdown != nil {
				proj = r.pushdown.Projection()
			}
			var vec vector.Any
			if r.pushdown != nil && r.pushdown.Unordered() {
				vecs, err := loader.LoadUnordered(r.q[:0], r.sctx, proj)
				if err != nil {
					return nil, err
				}
				vec = vecs[0]
				r.q = vecs[1:]
			} else {
				vec, err = loader.Load(r.sctx, proj)
				if err != nil {
					return nil, err
				}
			}
			n := int64(vec.Len())
			r.dispatcher.progress.Add(vio.Progress{RecordsRead: n, RecordsMatched: n})
			return vec, nil
		case *bsup.RowFrame:
			vec, err := frame.Deserialize()
			if err != nil {
				return nil, err
			}
			n := int64(vec.Len())
			size := int64(frame.Size())
			r.dispatcher.progress.Add(vio.Progress{RecordsRead: n, RecordsMatched: n, BytesMatched: size, BytesRead: size})
			return vec, nil
		default:
			panic(frame)
		}
	}
}

type frame struct {
	frame bsup.Frame
	err   error
}

func skipFrame(sctx *super.Context, mf *metafilter, frame *bsup.ColFrame) bool {
	vals := frame.ProjectMetadata(sctx, mf.projection)
	for _, val := range vals {
		if !mf.filter.Eval(val).Equal(super.False) {
			return false
		}
	}
	return true
}
