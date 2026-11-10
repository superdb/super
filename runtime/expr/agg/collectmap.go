package agg

import (
	"github.com/superdb/super"
	samagg "github.com/superdb/super/runtime/sam/expr/agg"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

type collectMap struct {
	samCollectMap *samagg.CollectMap
}

func newCollectMap() *collectMap {
	return &collectMap{samagg.NewCollectMap()}
}

func (c *collectMap) Consume(vec vector.Any) {
	if k := vec.Kind(); k == vector.KindNull || k == vector.KindError || k == vector.KindNone {
		return
	}
	typ := vec.Type()
	var b scode.Builder
	for i := range vec.Len() {
		b.Truncate()
		vec.Serialize(&b, i)
		c.samCollectMap.Consume(super.NewValue(typ, b.Bytes().Body()))
	}
}

func (c *collectMap) Result(sctx *super.Context) vector.Any {
	val := c.samCollectMap.Result(sctx)
	return sbuf.Dematerialize(sctx, val)
}

func (c *collectMap) ConsumeAsPartial(partial vector.Any) {
	c.Consume(partial)
}

func (c *collectMap) ResultAsPartial(sctx *super.Context) vector.Any {
	val := c.samCollectMap.ResultAsPartial(sctx)
	return sbuf.Dematerialize(sctx, val)
}
