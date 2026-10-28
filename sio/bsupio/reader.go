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
		var err error
		if fit, err = filteredSeekable(sctx, ra, p, progress); err != nil {
			return nil, err
		}
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
	ctx        context.Context
	sctx       *super.Context
	dispatcher *dispatcher
	progress   *vio.Progress

	searchFilter       vio.SearchEvaluator
	metaFilter         *vio.ValueFilter
	dataFilter         *vio.Filter
	projection         field.Projection
	unordered          bool
	dfProjContainsProj bool

	q []vector.Any
}

func newReader(ctx context.Context, sctx *super.Context, d *dispatcher, pushdown vio.Pushdown, progress *vio.Progress) (reader, error) {
	var dataFilter *vio.Filter
	var searchFilter vio.SearchEvaluator
	var projection field.Projection
	var unordered bool
	if pushdown != nil {
		var err error
		dataFilter, err = pushdown.DataFilter()
		if err != nil {
			return reader{}, err
		}
		searchFilter, err = pushdown.SearchFilter()
		if err != nil {
			return reader{}, err
		}
		projection = pushdown.Projection()
		unordered = pushdown.Unordered()
	}
	return reader{
		ctx:                ctx,
		sctx:               sctx,
		dispatcher:         d,
		progress:           progress,
		dataFilter:         dataFilter,
		searchFilter:       searchFilter,
		projection:         projection,
		unordered:          unordered,
		dfProjContainsProj: dataFilter != nil && dataFilter.Projection.Contains(projection),
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
	if vec, ok := r.nextUnorderedVec(); ok {
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
			r.progress.Add(vio.Progress{
				BytesScanned:     int64(frame.Size()),
				FramesScanned:    1,
				MetaBytesLoaded:  int64(frame.MetaSize()),
				TypesBytesLoaded: int64(frame.TypeSize()),
			})
			loader := loader.NewFrameLoader(r.sctx, frame)
			if r.searchFilter != nil && !loader.Search(r.searchFilter) {
				atomic.AddInt64(&r.progress.FramesSkippedSearchFilter, 1)
				continue
			}
			if r.unordered {
				r.q = r.q[:0]
				var valuesScanned int64
				for l := range loader.UnorderedLoaders() {
					if vec, ok := r.loadAndFilter(l); ok {
						r.q = append(r.q, vec)
						valuesScanned += int64(vec.Len())
					}
				}
				valuesLoaded := int64(loader.Len())
				vec, ok := r.nextUnorderedVec()
				if !ok {
					atomic.AddInt64(&r.progress.FramesSkippedDataFilter, 1)
					atomic.AddInt64(&r.progress.ValuesSkippedDataFilter, valuesLoaded)
					continue
				}
				if valuesLoaded > valuesScanned {
					atomic.AddInt64(&r.progress.ValuesSkippedDataFilter, valuesLoaded-valuesScanned)
				}
				atomic.AddInt64(&r.progress.ValuesScanned, valuesScanned)
				return vec, nil
			} else {
				valuesLoaded := int64(loader.Len())
				vec, ok := r.loadAndFilter(loader)
				if !ok {
					atomic.AddInt64(&r.progress.FramesSkippedDataFilter, 1)
					atomic.AddInt64(&r.progress.ValuesSkippedDataFilter, valuesLoaded)
					continue
				}
				valuesScanned := int64(vec.Len())
				if valuesLoaded > valuesScanned {
					atomic.AddInt64(&r.progress.ValuesSkippedDataFilter, valuesLoaded-valuesScanned)
				}
				atomic.AddInt64(&r.progress.ValuesScanned, valuesScanned)
				return vec, nil
			}
		case *bsup.RowFrame:
			vec, err := frame.Deserialize()
			if err != nil {
				return nil, err
			}
			r.progress.Add(vio.Progress{
				BytesScanned:     int64(frame.Size()),
				FramesScanned:    1,
				TypesBytesLoaded: int64(frame.TypeSize()),
			})
			valuesLoaded := int64(vec.Len())
			if r.dataFilter != nil {
				if index, ok := r.evalDataFilter(vec); !ok {
					atomic.AddInt64(&r.progress.FramesSkippedDataFilter, 1)
					atomic.AddInt64(&r.progress.ValuesSkippedDataFilter, valuesLoaded)
					continue
				} else if n := len(index); n > 0 {
					vec = vector.Pick(vec, index)
					atomic.AddInt64(&r.progress.ValuesSkippedDataFilter, valuesLoaded-int64(n))
				}
			}
			atomic.AddInt64(&r.progress.ValuesScanned, int64(vec.Len()))
			return vec, nil
		default:
			panic(frame)
		}
	}
}

func (r *reader) loadAndFilter(loader loader.Loader) (vector.Any, bool) {
	var vec vector.Any
	var index []uint32
	if df := r.dataFilter; df != nil {
		if df.Projection.IsEmpty() {
			vec = vector.NewNull(loader.Len())
		} else {
			vec = loader.Load(df.Projection)
		}
		var ok bool
		index, ok = r.evalDataFilter(vec)
		if !ok {
			return nil, false
		}
	}
	if r.projection.IsEmpty() {
		if n := len(index); n > 0 {
			// Creating a new vector is cheaper than vector.Pick.
			vec = vector.NewNull(uint32(n))
		} else if vec == nil {
			vec = vector.NewNull(loader.Len())
		}
	} else {
		// If the data filter projection contains the query projection, then optimize by
		// returning vec from the data filter projection.
		if vec == nil || !r.dfProjContainsProj {
			vec = loader.Load(r.projection)
		}
		if len(index) > 0 {
			vec = vector.Pick(vec, index)
		}
	}
	return vec, true
}

func (r *reader) evalDataFilter(vec vector.Any) (pickIndex []uint32, ok bool) {
	result := r.dataFilter.Expr.Eval(vec)
	bits, _ := expr.BoolMask(result)
	if bits.IsEmpty() {
		return nil, false
	}
	if bits.GetCardinality() == uint64(vec.Len()) {
		return nil, true
	}
	return bits.ToArray(), true
}

func (r *reader) nextUnorderedVec() (vector.Any, bool) {
	// Order doesn't matter here so peel the vectors off end of q
	// so we can reuse the underlying slice for the next set of vectors.
	n := len(r.q)
	if n == 0 {
		return nil, false
	}
	vec := r.q[n-1]
	r.q = r.q[:n-1]
	return vec, true
}

type frame struct {
	frame bsup.Frame
	err   error
}

func filteredSeekable(sctx *super.Context, s io.ReaderAt, p vio.Pushdown, progress *vio.Progress) (bsup.FrameIter, error) {
	seekable := bsup.NewSeekable(sctx, s)
	if p == nil {
		return seekable, nil
	}
	mf, err := p.MetaFilter()
	if mf == nil || err != nil {
		return seekable, err
	}
	var skipped int64
	offs, ok, err := seekable.FilterFrames(sctx, mf.Projection, func(val super.Value) bool {
		skip := mf.Expr.Eval(val).Equal(super.False)
		if skip {
			skipped++
		}
		return skip
	})
	if err != nil || !ok {
		return seekable, err
	}
	atomic.AddInt64(&progress.FramesSkippedMetaFilter, skipped)
	return bsup.NewOffsetsSeekable(seekable, offs), nil
}
