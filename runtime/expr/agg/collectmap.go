package agg

import (
	"github.com/superdb/super"
	samagg "github.com/superdb/super/runtime/sam/expr/agg"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

type collectMap struct {
	maps []*samagg.CollectMap
}

func newCollectMap() *collectMap {
	return &collectMap{}
}

func (c *collectMap) Resize(n uint32) {
	for range n - uint32(len(c.maps)) {
		c.maps = append(c.maps, samagg.NewCollectMap())
	}
}

func (c *collectMap) Consume(groups []uint32, vec vector.Any) {
	// XXX THIS - missing should be error
	if k := vec.Kind(); k == vector.KindNull || k == vector.KindError || k == vector.KindMissing {
		return
	}
	typ := vec.Type()
	var b scode.Builder
	for slot, g := range orZeros(groups, vec.Len()) {
		b.Truncate()
		vec.Serialize(&b, uint32(slot))
		c.maps[g].Consume(super.NewValue(typ, b.Bytes().Body()))
	}
}

func (c *collectMap) Result(sctx *super.Context) vector.Any {
	b := vector.NewDynamicValueBuilder()
	for _, m := range c.maps {
		b.Write(m.Result(sctx))
	}
	return b.Build(sctx)
}

func (c *collectMap) ConsumeAsPartial(groups []uint32, partial vector.Any) {
	c.Consume(groups, partial)
}

func (c *collectMap) ResultAsPartial(sctx *super.Context) vector.Any {
	b := vector.NewDynamicValueBuilder()
	for _, m := range c.maps {
		b.Write(m.ResultAsPartial(sctx))
	}
	return b.Build(sctx)
}
