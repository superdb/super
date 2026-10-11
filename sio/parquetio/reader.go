package parquetio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/superdb/super"
	"github.com/superdb/super/pkg/byteconv"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sio/arrowio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
	"golang.org/x/exp/constraints"
)

//lint:ignore ST1005 Parquet should be capitalized
var errNotSeekable = errors.New("Parquet format requires seekable input")

type Reader struct {
	ctx  context.Context
	sctx *super.Context

	fr                 *pqarrow.FileReader
	colIndexes         []int
	metadataColIndexes []int
	metadataFilters    []expr.Evaluator

	nextRowGroup *atomic.Int64
	rrs          []pqarrow.RecordReader
	vbs          []vectorBuilder
}

func NewReader(ctx context.Context, sctx *super.Context, r io.Reader, p vio.Pushdown, concurrentReaders int) (*Reader, error) {
	if concurrentReaders < 1 {
		panic(concurrentReaders)
	}
	ras, ok := r.(parquet.ReaderAtSeeker)
	if !ok {
		return nil, errNotSeekable
	}
	pr, err := file.NewParquetReader(ras)
	if err != nil {
		return nil, err
	}
	pqprops := pqarrow.ArrowReadProperties{
		Parallel:  true,
		BatchSize: 16184,
	}
	fr, err := pqarrow.NewFileReader(pr, pqprops, &poolAllocator{})
	if err != nil {
		return nil, err
	}
	var metadataColIndexes []int
	var metadataFilters []expr.Evaluator
	var fields []field.Path
	if p != nil {
		fields = p.Projection().Paths()
		filter, err := p.MetaFilter()
		if err != nil {
			return nil, err
		}
		if filter != nil {
			paths := filter.Projection.Paths()
			for i, p := range paths {
				// Trim trailing "max" or "min".
				paths[i] = p[:len(p)-1]
			}
			colIndexes := columnIndexes(fr.Manifest, paths)
			// Remove duplicates created above by trimming "max" and "min".
			metadataColIndexes = slices.Compact(colIndexes)
			for range concurrentReaders {
				filter, err := p.MetaFilter()
				if err != nil {
					return nil, err
				}
				metadataFilters = append(metadataFilters, filter.Expr)
			}
		}
	}
	var vbs []vectorBuilder
	for range concurrentReaders {
		vbs = append(vbs, vectorBuilder{sctx, map[arrow.DataType]super.Type{}})
	}
	return &Reader{
		ctx:                ctx,
		sctx:               sctx,
		fr:                 fr,
		colIndexes:         columnIndexes(fr.Manifest, fields),
		metadataColIndexes: metadataColIndexes,
		metadataFilters:    metadataFilters,
		nextRowGroup:       &atomic.Int64{},
		rrs:                make([]pqarrow.RecordReader, concurrentReaders),
		vbs:                vbs,
	}, nil
}

func columnIndexes(sm *pqarrow.SchemaManifest, fields []field.Path) []int {
	var indexes []int
	for _, f := range fields {
		indexes = appendColumnIndexesForField(indexes, sm.Fields, f)
	}
	return indexes
}

func appendColumnIndexesForField(indexes []int, sfs []pqarrow.SchemaField, f field.Path) []int {
	for _, sf := range sfs {
		if sf.Field.Name == f[0] {
			if len(f) == 1 {
				return appendColumnIndexes(indexes, &sf)
			}
			return appendColumnIndexesForField(indexes, sf.Children, f[1:])
		}
	}
	return indexes
}

func appendColumnIndexes(indexes []int, sf *pqarrow.SchemaField) []int {
	if sf.IsLeaf() {
		return append(indexes, sf.ColIndex)
	}
	for _, sf := range sf.Children {
		indexes = appendColumnIndexes(indexes, &sf)
	}
	return indexes
}

func (r *Reader) Pull(done bool) (vector.Any, error) {
	return r.ConcurrentPull(done, 0)
}

func (r *Reader) ConcurrentPull(done bool, id int) (vector.Any, error) {
	if done {
		return nil, nil
	}
	for {
		if err := r.ctx.Err(); err != nil {
			return nil, err
		}
		if r.rrs[id] == nil {
			pr := r.fr.ParquetReader()
			rowGroup := int(r.nextRowGroup.Add(1) - 1)
			if rowGroup >= pr.NumRowGroups() {
				return nil, nil
			}
			if len(r.metadataFilters) > 0 {
				rgMetadata := pr.MetaData().RowGroup(rowGroup)
				colIndexToField := r.fr.Manifest.ColIndexToField
				val := buildMetadataValue(r.sctx, rgMetadata, r.metadataColIndexes, colIndexToField)
				if r.metadataFilters[id].Eval(val).Equal(super.False) {
					continue
				}
			}
			rr, err := r.fr.GetRecordReader(r.ctx, r.colIndexes, []int{rowGroup})
			if err != nil {
				return nil, err
			}
			r.rrs[id] = rr
		}
		batch, err := r.rrs[id].Read()
		if err != nil {
			if err == io.EOF {
				r.rrs[id] = nil
				continue
			}
			return nil, err
		}
		return r.vbs[id].build(array.RecordToStructArray(batch), false)
	}
}

func (r *Reader) Type() (super.Type, error) {
	s, err := r.fr.Schema()
	if err != nil {
		return nil, err
	}
	return arrowio.NewTypeFromSchema(r.sctx, s)
}

type vectorBuilder struct {
	sctx  *super.Context
	types map[arrow.DataType]super.Type
}

func (v *vectorBuilder) build(a arrow.Array, nullable bool) (vector.Any, error) {
	dt := a.DataType()
	length := uint32(a.Len())
	var out vector.Any
	// Order here follows that of the arrow.Type constants.
	switch dt.ID() {
	case arrow.NULL:
		return vector.NewNull(length), nil
	case arrow.BOOL:
		vec := vector.NewFalse(length)
		arr := a.(*array.Boolean)
		for i := range length {
			if arr.Value(int(i)) {
				vec.Set(i)
			}
		}
		out = vec
	case arrow.UINT8:
		values := convertSlice[uint64](a.(*array.Uint8).Uint8Values())
		out = vector.NewUint(super.TypeUint8, values)
	case arrow.INT8:
		values := convertSlice[int64](a.(*array.Int8).Int8Values())
		out = vector.NewInt(super.TypeInt8, values)
	case arrow.UINT16:
		values := convertSlice[uint64](a.(*array.Uint16).Uint16Values())
		out = vector.NewUint(super.TypeUint16, values)
	case arrow.INT16:
		values := convertSlice[int64](a.(*array.Int16).Int16Values())
		out = vector.NewInt(super.TypeInt16, values)
	case arrow.UINT32:
		values := convertSlice[uint64](a.(*array.Uint32).Uint32Values())
		out = vector.NewUint(super.TypeUint32, values)
	case arrow.INT32:
		values := convertSlice[int64](a.(*array.Int32).Int32Values())
		out = vector.NewInt(super.TypeInt32, values)
	case arrow.UINT64:
		values := a.(*array.Uint64).Uint64Values()
		out = vector.NewUint(super.TypeUint64, values)
	case arrow.INT64:
		values := a.(*array.Int64).Int64Values()
		out = vector.NewInt(super.TypeInt64, values)
	case arrow.FLOAT16:
		values := make([]float64, length)
		for i, v := range a.(*array.Float16).Values() {
			values[i] = float64(v.Float32())
		}
		out = vector.NewFloat(super.TypeFloat16, values)
	case arrow.FLOAT32:
		values := convertSlice[float64](a.(*array.Float32).Float32Values())
		out = vector.NewFloat(super.TypeFloat32, values)
	case arrow.FLOAT64:
		values := a.(*array.Float64).Float64Values()
		out = vector.NewFloat(super.TypeFloat64, values)
	case arrow.STRING:
		arr := a.(*array.String)
		offsets := byteconv.ReinterpretSlice[uint32](arr.ValueOffsets())
		out = vector.NewString(vector.NewBytesTable(offsets, arr.ValueBytes()))
	case arrow.BINARY:
		arr := a.(*array.Binary)
		offsets := byteconv.ReinterpretSlice[uint32](arr.ValueOffsets())
		out = vector.NewBytes(vector.NewBytesTable(offsets, arr.ValueBytes()))
	case arrow.FIXED_SIZE_BINARY:
		value0 := a.(*array.FixedSizeBinary).Value(0)
		bytes := value0[:int(length)*len(value0)]
		offsets := make([]uint32, length+1)
		for i := range offsets {
			offsets[i] = uint32(i * len(value0))
		}
		out = vector.NewBytes(vector.NewBytesTable(offsets, bytes))
	case arrow.DATE32:
		values := make([]int64, length)
		for i, v := range a.(*array.Date32).Date32Values() {
			values[i] = int64(v) * int64(24*time.Hour)
		}
		out = vector.NewInt(super.TypeTime, values)
	case arrow.TIMESTAMP:
		multiplier := dt.(*arrow.TimestampType).TimeUnit().Multiplier()
		values := byteconv.ReinterpretSlice[int64](a.(*array.Timestamp).TimestampValues())
		if multiplier > 1 {
			for i := range values {
				values[i] *= int64(multiplier)
			}
		}
		out = vector.NewInt(super.TypeTime, values)
	case arrow.TIME32:
		multiplier := dt.(*arrow.Time32Type).TimeUnit().Multiplier()
		values := make([]int64, length)
		for i, v := range a.(*array.Time32).Time32Values() {
			values[i] = int64(v) * int64(multiplier)
		}
		out = vector.NewInt(super.TypeTime, values)
	case arrow.TIME64:
		multiplier := dt.(*arrow.Time64Type).TimeUnit().Multiplier()
		values := byteconv.ReinterpretSlice[int64](a.(*array.Time64).Time64Values())
		if multiplier > 1 {
			for i := range values {
				values[i] *= int64(multiplier)
			}
		}
		out = vector.NewInt(super.TypeTime, values)
	case arrow.DECIMAL128:
		typ, ok := v.types[dt]
		if !ok {
			d := dt.(arrow.DecimalType)
			name := fmt.Sprintf("deciaml_%d_%d", d.GetScale(), d.GetPrecision())
			var err error
			typ, err = v.sctx.LookupTypeNamed(name, super.TypeFloat64)
			if err != nil {
				return nil, err
			}
			v.types[dt] = typ
		}
		scale := dt.(arrow.DecimalType).GetScale()
		values := make([]float64, length)
		for i, v := range a.(*array.Decimal128).Values() {
			values[i] = v.ToFloat64(scale)
		}
		out = vector.NewFloat(typ, values)
	case arrow.DECIMAL256:
		typ, ok := v.types[dt]
		if !ok {
			d := dt.(arrow.DecimalType)
			name := fmt.Sprintf("deciaml_%d_%d", d.GetScale(), d.GetPrecision())
			var err error
			typ, err = v.sctx.LookupTypeNamed(name, super.TypeFloat64)
			if err != nil {
				return nil, err
			}
			v.types[dt] = typ
		}
		scale := dt.(arrow.DecimalType).GetScale()
		values := make([]float64, length)
		for i, v := range a.(*array.Decimal256).Values() {
			values[i] = v.ToFloat64(scale)
		}
		out = vector.NewFloat(typ, values)
	case arrow.LIST:
		arr := a.(*array.List)
		nullable := dt.(*arrow.ListType).ElemField().Nullable
		values, err := v.build(arr.ListValues(), nullable)
		if err != nil {
			return nil, err
		}
		offsets := byteconv.ReinterpretSlice[uint32](arr.Offsets())
		typ, ok := v.types[dt]
		if !ok {
			typ = v.sctx.LookupTypeArray(values.Type())
			v.types[dt] = typ
		}
		out = vector.NewArray(typ.(*super.TypeArray), offsets, values)
	case arrow.STRUCT:
		arr := a.(*array.Struct)
		arrowStructType := dt.(*arrow.StructType)
		fieldVecs := make([]vector.Any, arr.NumField())
		for i := range arr.NumField() {
			vec, err := v.build(arr.Field(i), arrowStructType.Field(i).Nullable)
			if err != nil {
				return nil, err
			}
			fieldVecs[i] = vec
		}
		typ, ok := v.types[dt]
		if !ok {
			fields := make([]super.Field, arr.NumField())
			for i, vec := range fieldVecs {
				fields[i] = super.NewField(arrowStructType.Field(i).Name, vec.Type())
			}
			arrowio.UniquifyFieldNames(fields)
			var err error
			typ, err = v.sctx.LookupTypeRecord(fields)
			if err != nil {
				return nil, err
			}
			v.types[dt] = typ
		}
		out = vector.NewRecord(typ.(*super.TypeRecord), fieldVecs, length)
	case arrow.MAP:
		arr := a.(*array.Map)
		keysNullable := dt.(*arrow.MapType).KeyField().Nullable
		keys, err := v.build(arr.Keys(), keysNullable)
		if err != nil {
			return nil, err
		}
		valsNullable := dt.(*arrow.MapType).ItemField().Nullable
		vals, err := v.build(arr.Items(), valsNullable)
		if err != nil {
			return nil, err
		}
		offsets := byteconv.ReinterpretSlice[uint32](arr.Offsets())
		typ, ok := v.types[dt]
		if !ok {
			typ = v.sctx.LookupTypeMap(keys.Type(), vals.Type())
			v.types[dt] = typ
		}
		out = vector.NewMap(typ.(*super.TypeMap), offsets, keys, vals)
	case arrow.FIXED_SIZE_LIST:
		arr := a.(*array.FixedSizeList)
		nullable := dt.(*arrow.FixedSizeListType).ElemField().Nullable
		values, err := v.build(arr.ListValues(), nullable)
		if err != nil {
			return nil, err
		}
		listLen := dt.(*arrow.FixedSizeListType).Len()
		offsets := make([]uint32, length+1)
		for i := range offsets {
			offsets[i] = uint32(i) * uint32(listLen)
		}
		typ, ok := v.types[dt]
		if !ok {
			typ = v.sctx.LookupTypeArray(values.Type())
			v.types[dt] = typ
		}
		out = vector.NewArray(typ.(*super.TypeArray), offsets, values)
	case arrow.LARGE_STRING:
		arr := a.(*array.LargeString)
		offsets := convertSlice[uint32](arr.ValueOffsets())
		for i, o := range arr.ValueOffsets() {
			if int64(offsets[i]) != o {
				return nil, fmt.Errorf("string offset exceeds uint32 range")
			}
		}
		out = vector.NewString(vector.NewBytesTable(offsets, arr.ValueBytes()))
	default:
		return nil, fmt.Errorf("unimplemented Parquet type %q", dt.Name())
	}
	if nullable {
		return v.buildNullableUnion(out, a), nil
	}
	return out, nil
}

func (v *vectorBuilder) buildNullableUnion(vec vector.Any, a arrow.Array) vector.Any {
	unionType := v.sctx.MustLookupTypeUnion([]super.Type{vec.Type(), super.TypeNull})
	numNulls := uint32(a.NullN())
	if numNulls == 0 {
		return vector.NewUnionOfOne(unionType, vec)
	} else if numNulls == vec.Len() {
		return vector.NewUnionOfOne(unionType, vector.NewNull(numNulls))
	}
	nullTag, vecTag, _ := arrowio.NullableUnionTagsAndType(unionType)
	tags := make([]uint32, vec.Len())
	vecIndex := make([]uint32, 0, vec.Len()-numNulls)
	for i := range vec.Len() {
		if a.IsNull(int(i)) {
			tags[i] = uint32(nullTag)
		} else {
			tags[i] = uint32(vecTag)
			vecIndex = append(vecIndex, i)
		}
	}
	var vecs [2]vector.Any
	vecs[nullTag] = vector.NewNull(numNulls)
	vecs[vecTag] = vector.Pick(vec, vecIndex)
	return vector.NewUnion(unionType, tags, vecs[:])
}

func convertSlice[Out, In constraints.Float | constraints.Integer](in []In) []Out {
	out := make([]Out, len(in))
	for i, v := range in {
		out[i] = Out(v)
	}
	return out
}
