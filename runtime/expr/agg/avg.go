package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/expr/cast"
	"github.com/superdb/super/vector"
)

type avg struct {
	sums   []float64
	counts []uint64
}

var _ expr.AggFunc = (*avg)(nil)

func (a *avg) Resize(n uint32) {
	a.sums = extend(a.sums, n)
	a.counts = extend(a.counts, n)
}

func (a *avg) Consume(groups []uint32, vec vector.Any) {
	vec = vector.Under(vec)
	if !super.IsNumber(vec.Type().ID()) {
		return
	}
	for _, g := range groups {
		a.counts[g]++
	}
	a.sum(groups, vec)
}

func (a *avg) sum(groups []uint32, vec vector.Any) {
	vec = cast.To(nil, vec, super.TypeFloat64)
	switch vec := vec.(type) {
	case *vector.Const:
		v := vec.Any.(*vector.Float).Values[0]
		for _, group := range groups {
			a.sums[group] += v
		}
	case *vector.Dict:
		vals := vec.Any.(*vector.Float).Values
		for slot, group := range groups {
			a.sums[group] += vals[vec.Index[slot]]
		}
	case *vector.View:
		vals := vec.Any.(*vector.Float).Values
		for slot, group := range groups {
			a.sums[group] += vals[vec.Index[slot]]
		}
	case *vector.Float:
		for slot, group := range groups {
			a.sums[group] += vec.Values[slot]
		}
	default:
		panic(vec)
	}
}

func (a *avg) Result(*super.Context) vector.Any {
	avgs := make([]float64, 0, len(a.counts))
	var nulls []uint32
	for g, count := range a.counts {
		if count == 0 {
			nulls = append(nulls, uint32(g))
			continue
		}
		avgs = append(avgs, a.sums[g]/float64(count))
	}
	out := vector.NewFloat(super.TypeFloat64, avgs)
	return combineNulls(out, nulls, uint32(len(a.counts)))
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
	sumVec := rec.Fields[si]
	countVec := rec.Fields[ci]
	if sumVec.Kind() != vector.KindFloat || countVec.Kind() != vector.KindUint {
		panic("avg: invalid partial")
	}
	sumVec, index := vector.UnwrapView(sumVec)
	sum := sumVec.(*vector.Float)
	for i, g := range groups {
		if index != nil {
			i = int(index[i])
		}
		a.sums[g] += sum.Values[i]
	}
	countVec, index = vector.UnwrapView(countVec)
	count := countVec.(*vector.Uint)
	for i, g := range groups {
		if index != nil {
			i = int(index[i])
		}
		a.counts[g] += count.Values[i]
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
