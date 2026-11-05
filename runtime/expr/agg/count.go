package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type count struct {
	counts []int64
}

func (a *count) Resize(n uint32) {
	if a.counts == nil {
		a.counts = make([]int64, n)
		return
	}
	a.counts = grow(a.counts, n)
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
			if groups == nil {
				a.counts[0] += int64(opt.SomeLen())
				return
			}
			for i, g := range groups {
				if some.Tags[i] == super.OptionSomeTag {
					a.counts[g]++
				}
			}
			return
		}
	}
	if groups == nil {
		a.counts[0] += int64(vec.Len())
		return
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
	if groups == nil {
		for i := range partial.Len() {
			a.counts[0] += vector.IntValue(partial, i)
		}
		return
	}
	base, index := flatten(partial)
	vals := base.(*vector.Int).Values
	if index == nil {
		for i, g := range groups {
			a.counts[g] += vals[i]
		}
		return
	}
	for i, g := range groups {
		a.counts[g] += vals[index[i]]
	}
}

func (a *count) ResultAsPartial(*super.Context) vector.Any {
	return a.Result(nil)
}
