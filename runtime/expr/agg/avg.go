package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

type avg struct {
	sums   []float64
	counts []uint64
}

var _ expr.AggFunc = (*avg)(nil)

func (a *avg) Resize(n uint32) {
	a.sums = append(a.sums, make([]float64, n-uint32(len(a.sums)))...)
	a.counts = append(a.counts, make([]uint64, n-uint32(len(a.counts)))...)
}

func (a *avg) Consume(groups []uint32, vec vector.Any) {
	vec = vector.Under(vec)
	if !super.IsNumber(vec.Type().ID()) {
		return
	}
	if groups == nil {
		a.counts[0] += uint64(vec.Len())
		a.sums[0] = sum(a.sums[0], vec)
		return
	}
	for _, g := range groups {
		a.counts[g]++
	}
	base, index := flatten(vec)
	foldNumeric(opSum, a.sums, nil, 0, groups, base, index)
}

func (a *avg) Result(*super.Context) vector.Any {
	tags := make([]uint8, len(a.counts))
	for g, count := range a.counts {
		if count > 0 {
			tags[g] = 1
		}
	}
	return buildByTag(tags, 2, func(tag uint8, groups []uint32) vector.Any {
		if tag == 0 {
			n := uint32(len(a.counts))
			if groups != nil {
				n = uint32(len(groups))
			}
			return vector.NewNull(n)
		}
		sums, counts := pick(a.sums, groups), pick(a.counts, groups)
		avgs := make([]float64, len(sums))
		for i := range avgs {
			avgs[i] = sums[i] / float64(counts[i])
		}
		return vector.NewFloat(super.TypeFloat64, avgs)
	})
}

const (
	sumName   = "sum"
	countName = "count"
)

func (a *avg) ConsumeAsPartial(groups []uint32, partial vector.Any) {
	rec, ok := vector.PushView(partial).(*vector.Record)
	if !ok {
		panic("avg: invalid partial")
	}
	si, ok1 := rec.Typ.IndexOfField(sumName)
	ci, ok2 := rec.Typ.IndexOfField(countName)
	if !ok1 || !ok2 {
		panic("avg: invalid partial")
	}
	sumVal := rec.Fields[si]
	countVal := rec.Fields[ci]
	if sumVal.Type() != super.TypeFloat64 || countVal.Type() != super.TypeUint64 {
		panic("avg: invalid partial")
	}
	for i, g := range orZeros(groups, partial.Len()) {
		a.sums[g] += vector.FloatValue(sumVal, uint32(i))
		a.counts[g] += vector.UintValue(countVal, uint32(i))
	}
}

func (a *avg) ResultAsPartial(sctx *super.Context) vector.Any {
	sum := vector.NewFloat(super.TypeFloat64, a.sums)
	count := vector.NewUint(super.TypeUint64, a.counts)
	typ := sctx.MustLookupTypeRecord([]super.Field{
		super.NewField(sumName, super.TypeFloat64),
		super.NewField(countName, super.TypeUint64),
	})
	return vector.NewRecord(typ, []vector.Any{sum, count}, uint32(len(a.sums)))
}
