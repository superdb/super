package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type Any struct {
	result vector.Any
}

func NewAny() *Any {
	return &Any{}
}

func (a *Any) NoRip() bool { return true }

func (a *Any) Consume(vec vector.Any) {
	if a.result != nil {
		return
	}
	slot := firstNonNullSlot(vec)
	if slot != -1 {
		if d, ok := vec.(*vector.Dynamic); ok {
			// Aggregators should return a definitive type so if we have a
			// dynamic unwrap the dynamic.
			vec = d.Values[d.Tags[slot]]
			slot = 0
		}
		a.result = vector.Pick(vec, []uint32{uint32(slot)})
	}
}

func firstNonNullSlot(vec vector.Any) int {
	if vec.Len() == 0 {
		return -1
	}
	if d, ok := vec.(*vector.Dynamic); ok {
		for i, vec := range d.Values {
			if slot := firstNonNullSlot(vec); slot != -1 {
				return int(d.ReverseTagMap()[i][slot])
			}
		}
		return -1
	}
	switch vec.Kind() {
	case vector.KindNull, vector.KindMissing:
		return -1
	case vector.KindFusion:
		return firstNonNullSlot(vector.Super(vec))
	case vector.KindUnion:
		return firstNonNullSlot(vec.(*vector.Union).Dynamic())
	default:
		return 0
	}
}

func (a *Any) ConsumeAsPartial(vec vector.Any) {
	a.Consume(vec)
}

func (a *Any) Result(sctx *super.Context) vector.Any {
	if a.result == nil {
		return vector.NewNull(1)
	}
	return a.result
}

func (a *Any) ResultAsPartial(*super.Context) vector.Any {
	return a.Result(nil)
}
