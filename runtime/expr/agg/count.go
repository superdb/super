package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type count struct {
	count int64
}

func (a *count) Consume(vec vector.Any) {
	switch vec.Kind() {
	// XXX TBD - missing should be error
	case vector.KindNull, vector.KindMissing:
		return
	case vector.KindOption:
		a.count += int64(vector.UnderOption(vec).SomeLen())
	default:
		a.count += int64(vec.Len())
	}
}

func (a *count) Result(*super.Context) vector.Any {
	return vector.NewInt(super.TypeInt64, []int64{a.count})
}

func (a *count) ConsumeAsPartial(partial vector.Any) {
	if partial.Len() != 1 || partial.Type() != super.TypeInt64 {
		panic("count: bad partial")
	}
	a.count += vector.IntValue(partial, 0)
}

func (a *count) ResultAsPartial(*super.Context) vector.Any {
	return a.Result(nil)
}
