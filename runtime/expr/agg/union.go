package agg

import (
	"github.com/superdb/super"
	samagg "github.com/superdb/super/runtime/sam/expr/agg"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

type union struct {
	unions []*samagg.Union
}

func newUnion() *union {
	return &union{}
}

func (u *union) Resize(n uint32) {
	for range n - uint32(len(u.unions)) {
		u.unions = append(u.unions, samagg.NewUnion())
	}
}

func (u *union) Consume(groups []uint32, vec vector.Any) {
	if vec.Kind() == vector.KindNone {
		return
	}
	typ := vec.Type()
	var b scode.Builder
	for slot, g := range groups {
		b.Truncate()
		vec.Serialize(&b, uint32(slot))
		u.unions[g].Update(typ, b.Bytes().Body())
	}
}

func (u *union) Result(sctx *super.Context) vector.Any {
	b := vector.NewDynamicValueBuilder()
	for _, union := range u.unions {
		b.Write(union.Result(sctx))
	}
	return b.Build(sctx)
}

func (u *union) ConsumeAsPartial(groups []uint32, partial vector.Any) {
	if partial.Kind() == vector.KindNull {
		return
	}
	elemGroups, values := expandContainer(groups, partial)
	typ := values.Type()
	utyp, _ := typ.(*super.TypeUnion)
	var b scode.Builder
	for slot, g := range elemGroups {
		b.Truncate()
		values.Serialize(&b, uint32(slot))
		typ, bytes := typ, b.Bytes().Body()
		if utyp != nil {
			typ, bytes = utyp.Untag(bytes)
		}
		u.unions[g].Update(typ, bytes)
	}
}

func (u *union) ResultAsPartial(sctx *super.Context) vector.Any {
	b := vector.NewDynamicValueBuilder()
	for _, union := range u.unions {
		b.Write(union.ResultAsPartial(sctx))
	}
	return b.Build(sctx)
}
