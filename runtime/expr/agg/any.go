package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

// Any is the any aggregate function, whose result is the first value that
// isn't null or none, or else none if it saw only plain nones (e.g., from
// ok() of an absent field), or else null.
type Any struct {
	val     super.Value
	sawNone bool
}

func NewAny() *Any {
	return &Any{}
}

func (a *Any) NoRip() bool { return true }

func (a *Any) Consume(vec vector.Any) {
	if a.val.IsValid() {
		return
	}
	switch vector.Super(vec).Kind() {
	case vector.KindNone:
		a.sawNone = true
		return
	case vector.KindNull:
		return
	}
	var b scode.Builder
	for slot := range vec.Len() {
		val := vector.ValueAt(&b, vec, slot)
		underVal := val.Under()
		if underVal.IsPlainNone() {
			a.sawNone = true
		} else if !underVal.IsNull() && !underVal.IsNone() {
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
		if a.sawNone {
			return vector.NewNone(1)
		}
		return vector.NewNull(1)
	}
	return sbuf.Dematerialize(sctx, a.val)
}

func (a *Any) ResultAsPartial(sctx *super.Context) vector.Any {
	return a.Result(sctx)
}
