package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

type Any struct {
	val super.Value
}

func NewAny() *Any {
	return &Any{}
}

func (a *Any) NoRip() bool { return true }

func (a *Any) Consume(vec vector.Any) {
	if a.val.IsValid() {
		return
	}
	if k := vector.Super(vec).Kind(); k == vector.KindNull || k == vector.KindMissing || k == vector.KindNone {
		return
	}
	var b scode.Builder
	for slot := range vec.Len() {
		val := vector.ValueAt(&b, vec, slot)
		if underVal := val.Under(); !underVal.IsNull() && !underVal.IsMissing() && !underVal.IsNone() {
			a.val = val
			return
		}
	}
}

func (a *Any) ConsumeAsPartial(vec vector.Any) {
	a.Consume(vec)
}

func (a *Any) Result(sctx *super.Context) vector.Any {
	if !a.val.IsValid() {
		return vector.NewNull(1)
	}
	return sbuf.Dematerialize(sctx, a.val)
}

func (a *Any) ResultAsPartial(sctx *super.Context) vector.Any {
	return a.Result(sctx)
}
