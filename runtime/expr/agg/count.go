package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type count struct {
	counts []int64
}

func (a *count) Resize(n uint32) {
	a.counts = extend(a.counts, n)
}

func (a *count) Consume(groups []uint32, vec vector.Any) {
	switch vec.Kind() {
	// XXX TBD - missing should be error
	case vector.KindNull, vector.KindNone:
		return
	case vector.KindOption:
		opt := vector.UnderOption(vec)
		switch some := opt.Any.(type) {
		case *vector.None:
			return
		case *vector.Dynamic:
			for i, g := range groups {
				if some.Tags[i] == super.OptionSomeTag {
					a.counts[g]++
				}
			}
			return
		}
	}
	for _, g := range groups {
		a.counts[g]++
	}
}

func (a *count) Result(*super.Context) vector.Any {
	return vector.NewInt(super.TypeInt64, a.counts)
}

func (a *count) ConsumeAsPartial(groups []uint32, partial vector.Any) {
	if partial.Type() != super.TypeInt64 {
		panic("count: bad partial")
	}
	partial, index := vector.UnwrapView(partial)
	vals := partial.(*vector.Int).Values
	for i, g := range groups {
		if index != nil {
			i = int(index[i])
		}
		a.counts[g] += vals[i]
	}
}

func (a *count) ResultAsPartial(*super.Context) vector.Any {
	return a.Result(nil)
}
