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
	table       *Table
	objects     []data.Object
	inputSorted bool
	ctx         context.Context
	sctx        *super.Context
	builder     *vbuild.DynamicBuilder
	stats       ImportStats
	closer      io.Closer
}

var _ vio.PushCloser = (*Writer)(nil)

func NewWriter(ctx context.Context, sctx *super.Context, table *Table) (*Writer, error) {
	return &Writer{
		table:   table,
		ctx:     ctx,
		sctx:    sctx,
		builder: vbuild.NewDynamicBuilder(),
	}, nil
}

// NewSortedWriter returns a Writer for input that is already in the table's
// sort order, such as a merge of the table's own data objects during
// compaction.  Such a writer never splits a run of equal sort keys across
// objects, so the objects it writes don't overlap.  An object may therefore
// exceed the table's ObjectCap when many values share a key.
func NewSortedWriter(ctx context.Context, sctx *super.Context, table *Table) (*Writer, error) {
	w, err := NewWriter(ctx, sctx, table)
	if err != nil {
		return nil, err
	}
	w.inputSorted = true
	return w, nil
}

func (w *Writer) newObject() *data.Object {
	w.objects = append(w.objects, data.NewObject())
	return &w.objects[len(w.objects)-1]
}

func (w *Writer) Push(vec vector.Any) error {
	w.builder.Write(vec)
	if uint64(w.builder.Len()) >= w.table.ObjectCap {
		return w.flush(false)
	}
	return nil
}

// flush writes the buffered values as data objects.  final is true when no
// more input will follow.
func (w *Writer) flush(final bool) error {
	// Each object is one big vector limited in length by ObjectCap.
	// Inside of the superframe are Frames which are in turn limited
	// in length by FrameCap.
	vec := w.builder.Build()
	w.builder = vbuild.NewDynamicBuilder()
	if vec.Len() == 0 {
		return nil
	}
	sortkey, sorted := w.table.SortKeys.Primary()
	if !sorted {
		return w.writeObject(vec, nil)
	}
	// first and last are the first and last keys in sort order.
	vec, first, last := w.sort(vec)
	if !w.inputSorted {
		return w.writeObject(vec, &keyRange{first, last, sortkey.Order})
	}
	return w.flushSorted(vec, sortkey.Path, sortkey.Order, final)
}

// flushSorted writes objects of about ObjectCap values from vec, which is in
// sort order, cutting only where the sort key changes so no key spans two
// objects.  Unless final is true, values after the last cut stay buffered
// since equal keys may still be arriving.
func (w *Writer) flushSorted(vec vector.Any, path field.Path, o order.Which, final bool) error {
	keys := w.deref(vec, path)
	key := func(slot uint32) super.Value {
		val := vector.ValueAt(nil, keys, slot)
		return val.MissingAsNull()
	}
	objectCap := uint32(min(w.table.ObjectCap, uint64(vec.Len())))
	n := vec.Len()
	var start uint32
	for objectCap > 0 && n-start > objectCap {
		k := start + objectCap
		// Since vec is sorted, if the last key equals the key at the cut
		// point, then so do all the keys in between and there's no cut.
		if key(k - 1).Equal(key(n - 1)) {
			break
		}
		for key(k).Equal(key(k - 1)) {
			k++
		}
		head := vector.Pick(vec, span(start, k))
		if err := w.writeObject(head, &keyRange{key(start), key(k - 1), o}); err != nil {
			return err
		}
		start = k
	}
	rest := vec
	if start > 0 {
		rest = vector.Pick(vec, span(start, n))
	}
	if !final {
		w.builder.Write(rest)
		return nil
	}
	return w.writeObject(rest, &keyRange{key(start), key(n - 1), o})
}

// keyRange holds the first and last sort keys of an object's values in
// sort order.
type keyRange struct {
	first, last super.Value
	order       order.Which
}

func (w *Writer) writeObject(vec vector.Any, keys *keyRange) error {
	object := w.newObject()
	if keys != nil {
		object.Min, object.Max = keys.first, keys.last
		if keys.order == order.Desc {
			object.Min, object.Max = object.Max, object.Min
		}
	}
	out, err := w.table.engine.Put(w.ctx, object.URI(w.table.DataPath))
	if err != nil {
		return err
	}
	writer := bsup.NewColumnWriterWithCap(out, w.table.FrameCap)
	size, err := writer.WriteSuperFrame(vec)
	if err != nil {
		out.Close()
		return err
	}
	object.Type = writer.FusedType()
	object.Count = uint64(vec.Len())
	object.Size = int64(size)
	w.stats.Accumulate(ImportStats{
		ObjectsWritten:     1,
		RecordBytesWritten: int64(size),
		RecordsWritten:     int64(vec.Len()),
	})
	return writer.Close()
}

func span(from, to uint32) []uint32 {
	index := make([]uint32, 0, to-from)
	for k := from; k < to; k++ {
		index = append(index, k)
	}
	return index
}

func (w *Writer) Close() error {
	return w.flush(true)
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

func ImportComparator(sctx *super.Context, table *Table) *samexpr.Comparator {
	var exprs []samexpr.SortExpr
	for _, s := range table.SortKeys {
		exprs = append(exprs, samexpr.NewSortExpr(samexpr.NewDottedExpr(sctx, s.Path), s.Order, s.Order.NullsMax(true)))
	}
	var o order.Which
	if sortKey, ok := table.SortKeys.Primary(); ok {
		o = sortKey.Order
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
	c := ImportComparator(w.sctx, w.table)
	sortKey, ok := w.table.SortKeys.Primary()
	if !ok {
		panic(w.table)
	}
	primaryKey := samexpr.NewDottedExpr(w.sctx, sortKey.Path)
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
	if len(w.table.SortKeys) != 1 {
		return nil, super.Value{}, super.Value{}
	}
	sortKey := w.table.SortKeys[0]
	return w.fastSortWithKey(vec, w.deref(vec, sortKey.Path), sortKey.Order)
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
	case *vector.UUID:
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
		return vector.NewView(vec, idx), super.NewUUID(minVal), super.NewUUID(maxVal)
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
