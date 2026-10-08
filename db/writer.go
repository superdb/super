package db

import (
	"bytes"
	"cmp"
	"context"
	"io"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime/expr"
	samexpr "github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
	"github.com/superdb/super/vector/vio"
)

type Writer struct {
	pool        *Pool
	objects     []data.Object
	inputSorted bool
	ctx         context.Context
	sctx        *super.Context
	builder     *vbuild.DynamicBuilder
	stats       ImportStats
	closer      io.Closer
}

var _ vio.PushCloser = (*Writer)(nil)

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
	// XXX We need a threshold here (or in BSUP column writer)
	// so we can write lots of smaller row-group-sized column frames
	// per super frame.  This will come in a subsequent PR.
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
	sortKey := &key
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
	w.stats.Accumulate(ImportStats{
		ObjectsWritten:     1,
		RecordBytesWritten: int64(size),
		RecordsWritten:     int64(vec.Len()),
	})
	w.builder = vbuild.NewDynamicBuilder()
	return writer.Close()
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

func ImportComparator(sctx *super.Context, pool *Pool) *samexpr.Comparator {
	var exprs []samexpr.SortExpr
	for _, s := range pool.SortKeys {
		exprs = append(exprs, samexpr.NewSortExpr(samexpr.NewDottedExpr(sctx, s.Key), s.Order, s.Order.NullsMax(true)))
	}
	var o order.Which
	if !pool.SortKeys.IsNil() {
		o = pool.SortKeys.Primary().Order
	}
	// valueAsBytes establishes a total order.
	exprs = append(exprs, samexpr.NewSortExpr(&valueAsBytes{}, o, o.NullsMax(true)))
	return samexpr.NewComparator(exprs...)
}

type valueAsBytes struct{}

func (v *valueAsBytes) Eval(val super.Value) super.Value {
	return super.NewBytes(val.Bytes())
}

func (w *Writer) sort(vec vector.Any) (vector.Any, super.Value, super.Value) {
	if vec, minVal, maxVal := w.fastSort(vec); vec != nil {
		return vec, minVal, maxVal
	}
	c := ImportComparator(w.sctx, w.pool)
	primaryKey := samexpr.NewDottedExpr(w.sctx, w.pool.SortKeys.Primary().Key)
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

func (w *Writer) fastSort(vec vector.Any) (vector.Any, super.Value, super.Value) {
	if len(w.pool.SortKeys) != 1 {
		return nil, super.Value{}, super.Value{}
	}
	sortKey := w.pool.SortKeys[0]
	return w.fastSortWithKey(vec, w.deref(vec, sortKey.Key), sortKey.Order)
}

func (w *Writer) fastSortWithKey(vec, key vector.Any, o order.Which) (vector.Any, super.Value, super.Value) {
	switch key := key.(type) {
	case *vector.Named:
		return w.fastSortWithKey(vec, key.Any, o)
	case *vector.Int:
		idx := make([]uint32, len(key.Values))
		for k := range idx {
			idx[k] = uint32(k)
		}
		slices.SortStableFunc(idx, func(a, b uint32) int {
			if o == order.Desc {
				a, b = b, a
			}
			return cmp.Compare(key.Values[a], key.Values[b])
		})
		minVal := key.Values[0]
		maxVal := key.Values[0]
		for _, val := range key.Values[1:] {
			if val < minVal {
				minVal = val
			} else if val > maxVal {
				maxVal = val
			}
		}
		if o == order.Desc {
			minVal, maxVal = maxVal, minVal
		}
		return vector.NewView(vec, idx), super.NewInt(key.Type(), minVal), super.NewInt(key.Type(), maxVal)
	case *vector.Uint:
		idx := make([]uint32, len(key.Values))
		for k := range idx {
			idx[k] = uint32(k)
		}
		slices.SortStableFunc(idx, func(a, b uint32) int {
			if o == order.Desc {
				a, b = b, a
			}
			return cmp.Compare(key.Values[a], key.Values[b])
		})
		minVal := key.Values[0]
		maxVal := key.Values[0]
		for _, val := range key.Values[1:] {
			if val < minVal {
				minVal = val
			} else if val > maxVal {
				maxVal = val
			}
		}
		if o == order.Desc {
			minVal, maxVal = maxVal, minVal
		}
		return vector.NewView(vec, idx), super.NewUint(key.Type(), minVal), super.NewUint(key.Type(), maxVal)
	case *vector.Float:
		idx := make([]uint32, len(key.Values))
		for k := range idx {
			idx[k] = uint32(k)
		}
		slices.SortStableFunc(idx, func(a, b uint32) int {
			if o == order.Desc {
				a, b = b, a
			}
			return cmp.Compare(key.Values[a], key.Values[b])
		})
		minVal := key.Values[0]
		maxVal := key.Values[0]
		for _, val := range key.Values[1:] {
			if val < minVal {
				minVal = val
			} else if val > maxVal {
				maxVal = val
			}
		}
		if o == order.Desc {
			minVal, maxVal = maxVal, minVal
		}
		return vector.NewView(vec, idx), super.NewFloat(key.Type(), minVal), super.NewFloat(key.Type(), maxVal)
	case *vector.String:
		idx := make([]uint32, key.Len())
		for k := range idx {
			idx[k] = uint32(k)
		}
		slices.SortStableFunc(idx, func(a, b uint32) int {
			if o == order.Desc {
				a, b = b, a
			}
			return strings.Compare(key.Value(a), key.Value(b))
		})
		minVal := key.Value(0)
		maxVal := key.Value(0)
		for k := range vec.Len() - 1 {
			off := k + 1
			if s := key.Value(off); s < minVal {
				minVal = s
			} else if s > maxVal {
				maxVal = s
			}
		}
		if o == order.Desc {
			minVal, maxVal = maxVal, minVal
		}
		return vector.NewView(vec, idx), super.NewString(minVal), super.NewString(maxVal)
	case *vector.Bytes:
		idx := make([]uint32, key.Len())
		for k := range idx {
			idx[k] = uint32(k)
		}
		slices.SortStableFunc(idx, func(a, b uint32) int {
			if o == order.Desc {
				a, b = b, a
			}
			return bytes.Compare(key.Value(a), key.Value(b))
		})
		minVal := key.Value(0)
		maxVal := key.Value(0)
		for k := range vec.Len() - 1 {
			off := k + 1
			if b := key.Value(off); bytes.Compare(b, minVal) < 0 {
				minVal = b
			} else if bytes.Compare(b, maxVal) > 0 {
				maxVal = b
			}
		}
		if o == order.Desc {
			minVal, maxVal = maxVal, minVal
		}
		return vector.NewView(vec, idx), super.NewBytes(minVal), super.NewBytes(maxVal)
	case *vector.IP:
		idx := make([]uint32, len(key.Values))
		for k := range idx {
			idx[k] = uint32(k)
		}
		slices.SortStableFunc(idx, func(a, b uint32) int {
			if o == order.Desc {
				a, b = b, a
			}
			return key.Values[a].Compare(key.Values[b])
		})
		minVal := key.Values[0]
		maxVal := key.Values[0]
		for _, val := range key.Values[1:] {
			if val.Compare(minVal) < 0 {
				minVal = val
			} else if val.Compare(maxVal) > 0 {
				maxVal = val
			}
		}
		if o == order.Desc {
			minVal, maxVal = maxVal, minVal
		}
		return vector.NewView(vec, idx), super.NewIP(minVal), super.NewIP(maxVal)
	}
	return nil, super.Value{}, super.Value{}
}

func (w *Writer) deref(vec vector.Any, path field.Path) vector.Any {
	e := expr.NewDottedExpr(w.sctx, path.Chain())
	return e.Eval(vec)
}
