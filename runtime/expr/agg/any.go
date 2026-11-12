package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

type Any struct {
	vals []super.Value // invalid until a group finds a value
}

func NewAny() *Any {
	return &Any{}
}

func (a *Any) NoRip() bool { return true }

func (a *Any) Resize(n uint32) {
	a.vals = extend(a.vals, n)
}

func (a *Any) Consume(groups []uint32, vec vector.Any) {
	if k := vector.Super(vec).Kind(); k == vector.KindNull || k == vector.KindMissing || k == vector.KindNone {
		return
	}
	var b scode.Builder
	for slot, g := range groups {
		if a.vals[g].IsValid() {
			continue
		}
		val := vector.ValueAt(&b, vec, uint32(slot))
		if underVal := val.Under(); !underVal.IsNull() && !underVal.IsMissing() && !underVal.IsNone() {
			a.vals[g] = val.Copy()
		}
	}
}

func (a *Any) ConsumeAsPartial(groups []uint32, vec vector.Any) {
	a.Consume(groups, vec)
}

func (a *Any) Result(sctx *super.Context) vector.Any {
	b := vector.NewDynamicValueBuilder()
	for _, val := range a.vals {
		if !val.IsValid() {
			val = super.Null
		}
		b.Write(val)
	}
	return b.Build(sctx)
}

func (a *Any) ResultAsPartial(sctx *super.Context) vector.Any {
	return a.Result(sctx)
}
