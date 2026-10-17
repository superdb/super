package agg

import (
	"github.com/superdb/super"
	samagg "github.com/superdb/super/runtime/sam/expr/agg"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

type union struct {
	samunion *samagg.Union
}

func newUnion() *union {
	return &union{samunion: samagg.NewUnion()}
}

func (u *union) Consume(vec vector.Any) {
	// XXX TBD - missing should be error
	if k := vec.Kind(); k == vector.KindMissing {
		return
	}
	switch vec := vec.(type) {
	case *vector.Const:
		val := vector.ValueAt(nil, vec, 0)
		u.samunion.Update(val.Type(), val.Bytes())
	case *vector.Dict:
		u.Consume(vec.Any)
	default:
		typ := vec.Type()
		var b scode.Builder
		for i := range vec.Len() {
			b.Truncate()
			vec.Serialize(&b, i)
			u.samunion.Update(typ, b.Bytes().Body())
		}
	}
}

func (u *union) Result(sctx *super.Context) vector.Any {
	val := u.samunion.Result(sctx)
	return sbuf.Dematerialize(sctx, val)
}

func (u *union) ConsumeAsPartial(partial vector.Any) {
	if partial.Kind() == vector.KindNull {
		return
	}
	n := partial.Len()
	var index []uint32
	if view, ok := partial.(*vector.View); ok {
		index = view.Index
		partial = view.Any
	}
	set, ok := partial.(*vector.Set)
	if !ok {
		panic("union: partial not a set type")
	}
	inner := set.Values
	typ := inner.Type()
	union, _ := typ.(*super.TypeUnion)
	var b scode.Builder
	for idx := range n {
		i := idx
		if index != nil {
			idx = index[i]
		}
		for k := set.Offsets[idx]; k < set.Offsets[idx+1]; k++ {
			b.Truncate()
			inner.Serialize(&b, k)
			bytes := b.Bytes().Body()
			if union != nil {
				typ, bytes = union.Untag(bytes)
			}
			u.samunion.Update(typ, bytes)
		}
	}
}

func (u *union) ResultAsPartial(sctx *super.Context) vector.Any {
	val := u.samunion.ResultAsPartial(sctx)
	return sbuf.Dematerialize(sctx, val)
}
