package db

import (
	"context"
	"sync/atomic"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/order"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
)

type Writer struct {
	pool        *Pool
	objects     []data.Object
	inputSorted bool
	ctx         context.Context
	sctx        *super.Context
	builder     *vbuild.DynamicBuilder
	stats       ImportStats
}

// XXX TODO: make this concurrent so we can be building and sorting vectors,
// while the previous super frame is being written to storage.
func NewWriter(ctx context.Context, sctx *super.Context, pool *Pool) (*Writer, error) {
	return &Writer{
		pool:    pool,
		ctx:     ctx,
		sctx:    sctx,
		builder: vbuild.NewDynamicBuilder(),
	}, nil
}

func (w *Writer) newObject() *data.Object {
	w.objects = append(w.objects, data.NewObject())
	return &w.objects[len(w.objects)-1]
}

func (w *Writer) Push(vec vector.Any) error {
	//XXX need thresh
	// Keep things simple for now... build one big vector then
	// sort if there's a sort key and compute min/max over the
	// vector when we push it to storage in BSUP columns format.
	w.builder.Write(vec)
	return nil
}

func (w *Writer) flush() error {
	vec := w.builder.Build()
	if vec.Len() == 0 {
		return nil
	}
	object := w.newObject()
	key := w.pool.SortKeys.Primary()
	sortKey := &key //XXX pool key should return ptr which is nil when no key
	out, err := w.pool.engine.Put(w.ctx, object.URI(w.pool.DataPath))
	if err != nil {
		return err
	}
	if sortKey != nil {
		var minVal, maxVal super.Value
		vec, minVal, maxVal = w.sort(vec)
		if sortKey.Order == order.Desc {
			minVal, maxVal = maxVal, minVal
		}
		object.Min = minVal
		object.Max = maxVal
	}
	writer := bsup.NewColumnWriter(out)
	size, err := writer.WriteSuperFrame(vec)
	if err != nil {
		out.Close()
		return err
	}
	object.Count = uint64(vec.Len())
	object.Size = int64(size)
	// XXX Record -> Values
	// we should also track BSUP column meta sizes vs data segment sizes so we
	// have a convenient place to grab these stats
	w.stats.Accumulate(ImportStats{
		ObjectsWritten:     1,
		RecordBytesWritten: int64(size),
		RecordsWritten:     int64(vec.Len()),
	})
	w.builder = vbuild.NewDynamicBuilder()
	return nil
}

func (w *Writer) Close() error {
	return w.flush()
}

func (w *Writer) Stats() ImportStats {
	return w.stats.Copy()
}

type ImportStats struct {
	ObjectsWritten     int64
	RecordBytesWritten int64
	RecordsWritten     int64
}

func (s *ImportStats) Accumulate(b ImportStats) {
	atomic.AddInt64(&s.ObjectsWritten, b.ObjectsWritten)
	atomic.AddInt64(&s.RecordBytesWritten, b.RecordBytesWritten)
	atomic.AddInt64(&s.RecordsWritten, b.RecordsWritten)
}

func (s *ImportStats) Copy() ImportStats {
	return ImportStats{
		ObjectsWritten:     atomic.LoadInt64(&s.ObjectsWritten),
		RecordBytesWritten: atomic.LoadInt64(&s.RecordBytesWritten),
		RecordsWritten:     atomic.LoadInt64(&s.RecordsWritten),
	}
}

func ImportComparator(sctx *super.Context, pool *Pool) *expr.Comparator {
	var exprs []expr.SortExpr
	for _, s := range pool.SortKeys {
		exprs = append(exprs, expr.NewSortExpr(expr.NewDottedExpr(sctx, s.Key), s.Order, s.Order.NullsMax(true)))
	}
	var o order.Which
	if !pool.SortKeys.IsNil() {
		o = pool.SortKeys.Primary().Order
	}
	// valueAsBytes establishes a total order.
	exprs = append(exprs, expr.NewSortExpr(&valueAsBytes{}, o, o.NullsMax(true)))
	return expr.NewComparator(exprs...)
}

type valueAsBytes struct{}

func (v *valueAsBytes) Eval(val super.Value) super.Value {
	return super.NewBytes(val.Bytes())
}

func (w *Writer) sort(vec vector.Any) (vector.Any, super.Value, super.Value) {
	c := ImportComparator(w.sctx, w.pool)
	primaryKey := expr.NewDottedExpr(w.sctx, w.pool.SortKeys.Primary().Key)
	reader := c.SortStableReader(sbuf.Materialize(vec).Values())
	out := vector.NewDynamicValueBuilder()
	var minVal, maxVal super.Value
	first := true
	for {
		val, err := reader.Read()
		if err != nil {
			panic(err)
		}
		if val == nil {
			return out.Build(w.sctx), minVal, maxVal
		}
		if first {
			first = false
			minVal = primaryKey.Eval(*val)
			minVal = minVal.MissingAsNull()
		}
		maxVal = primaryKey.Eval(*val)
		maxVal = maxVal.MissingAsNull()
		out.Write(*val)
	}
}
