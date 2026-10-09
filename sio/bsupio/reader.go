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
	"github.com/superdb/super/bsup/loader"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

func NewReader(ctx context.Context, sctx *super.Context, r io.Reader, p vio.Pushdown, progress *vio.Progress, concurrentReaders int) (vio.Puller, error) {
	if progress == nil {
		progress = &vio.Progress{}
	}
	var fit bsup.FrameIter
	if ra, ok := readerAt(r); ok {
		fit = bsup.NewSeekable(sctx, ra)
	} else {
		fit = bsup.NewStream(sctx, r)
	}
	return newDispatcher(ctx, sctx, fit, p, progress, concurrentReaders)
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

func NewValueReader(ctx context.Context, sctx *super.Context, r io.Reader) (sio.Reader, error) {
	reader, err := NewReader(ctx, sctx, r, nil, nil, 1)
	if err != nil {
		return nil, err
	}
	return sbuf.PullerReader(sbuf.NewMaterializer(reader)), nil
}

type dispatcher struct {
	ctx      context.Context
	sctx     *super.Context
	pushdown vio.Pushdown

	readers []reader
	frameCh chan frame
	fit     bsup.FrameIter
	once    sync.Once
}

var _ sio.Typer = (*dispatcher)(nil)

func newDispatcher(ctx context.Context, sctx *super.Context, fit bsup.FrameIter, p vio.Pushdown, progress *vio.Progress, concurrentReaders int) (*dispatcher, error) {
	if concurrentReaders < 1 {
		panic(concurrentReaders)
	}
	frameCh := make(chan frame, runtime.GOMAXPROCS(0))
	d := &dispatcher{
		ctx:      ctx,
		sctx:     sctx,
		pushdown: p,
		frameCh:  frameCh,
		fit:      fit,
	}
	for range concurrentReaders {
		r, err := newReader(ctx, sctx, d, p, progress)
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

func (d *dispatcher) next() (bsup.Frame, error) {
	d.once.Do(func() {
		go func() {
			defer close(d.frameCh)
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
				if f == nil || err != nil {
					return
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
	metaFilter *vio.ValueFilter
	dataFilter *vio.Filter
	pushdown   vio.Pushdown
	progress   *vio.Progress
	q          []vector.Any
}

func newReader(ctx context.Context, sctx *super.Context, d *dispatcher, pushdown vio.Pushdown, progress *vio.Progress) (reader, error) {
	var metaFilter *vio.ValueFilter
	var dataFilter *vio.Filter
	if pushdown != nil {
		var err error
		metaFilter, err = pushdown.MetaFilter()
		if err != nil {
			return reader{}, err
		}
		dataFilter, err = pushdown.DataFilter()
		if err != nil {
			return reader{}, err
		}
	}
	return reader{
		ctx:        ctx,
		sctx:       sctx,
		dispatcher: d,
		metaFilter: metaFilter,
		dataFilter: dataFilter,
		pushdown:   pushdown,
		progress:   progress,
	}, nil
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
		r.progress.Add(vio.Progress{ValuesScanned: n})
		return vec, nil
	}
	for {
		frame, err := r.dispatcher.next()
		if frame == nil || err != nil {
			return nil, err
		}
		switch frame := frame.(type) {
		case *bsup.ColFrame:
			// Arrange for ColFrame to update the types bytes read and the
			// bytes loaded from segments in the data section.
			frame.LinkProgress(r.progress)
			// XXX using the query context for the metadata filter unnecessarily
			// pollutes the type context.  We should use the BSUP local context for
			// this filtering but this will require a little compiler refactoring to be
			// able to build runtime expressions that use different type contexts.
			size := int64(frame.Size())
			r.progress.Add(vio.Progress{
				BytesScanned:     size,
				FramesScanned:    1,
				MetaBytesLoaded:  int64(frame.MetaSize()),
				TypesBytesLoaded: int64(frame.TypeSize()),
			})
			if r.metaFilter != nil && evalMetaFilter(r.sctx, r.metaFilter, frame) {
				atomic.AddInt64(&r.progress.FramesSkippedMetaFilter, 1)
				continue
			}
			var pick []uint32
			loader := loader.NewFrameLoader(r.sctx, frame)
			if r.dataFilter != nil && len(r.dataFilter.Projection) != 0 {
				skip, idx, err := r.evalDataFilter(loader)
				if err != nil {
					return nil, err
				}
				if skip {
					atomic.AddInt64(&r.progress.ValuesSkippedDataFilter, int64(loader.Len()))
					atomic.AddInt64(&r.progress.FramesSkippedDataFilter, 1)
					continue
				}
				pick = idx
				atomic.AddInt64(&r.progress.ValuesSkippedDataFilter, int64(loader.Len()-uint32(len(pick))))
			}
			var proj field.Projection
			var none bool
			if r.pushdown != nil {
				proj = r.pushdown.Projection()
				if proj != nil && len(proj) == 0 {
					none = true
				}
			}
			var vec vector.Any
			if none {
				vec = vector.NewNull(loader.Len())
			} else {
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
					if pick != nil {
						vec = vector.Pick(vec, pick)
					}
				}
			}
			n := int64(vec.Len())
			atomic.AddInt64(&r.progress.ValuesScanned, n)
			return vec, nil
		case *bsup.RowFrame:
			vec, err := frame.Deserialize()
			if err != nil {
				return nil, err
			}
			n := int64(vec.Len())
			size := int64(frame.Size())
			typeSize := int64(frame.TypeSize())
			r.progress.Add(vio.Progress{
				BytesScanned:     size,
				ValuesScanned:    n,
				FramesScanned:    1,
				TypesBytesLoaded: typeSize,
			})
			return vec, nil
		default:
			panic(frame)
		}
	}
}

func (r *reader) evalDataFilter(loader *loader.FrameLoader) (bool, []uint32, error) {
	vec, err := loader.Load(r.sctx, r.dataFilter.Projection)
	if err != nil {
		return false, nil, err
	}
	result := r.dataFilter.Expr.Eval(vec)
	// XXX should pass down a flag here to ignore errors since this can happen often in fast path
	bits, _ := expr.BoolMask(result)
	if bits.IsEmpty() {
		return true, nil, nil
	}
	if bits.GetCardinality() == uint64(result.Len()) {
		return false, nil, nil
	}
	// Return the pick index for stuff not ruled out.
	return false, bits.ToArray(), nil
}

type frame struct {
	frame bsup.Frame
	err   error
}

func evalMetaFilter(sctx *super.Context, mf *vio.ValueFilter, frame *bsup.ColFrame) bool {
	vals := frame.ProjectMetadata(sctx, mf.Projection)
	for _, val := range vals {
		if !mf.Expr.Eval(val).Equal(super.False) {
			return false
		}
	}
	return true
}
