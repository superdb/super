package agg

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
)

type collect struct {
	builder *vbuild.DynamicBuilder
	defuse  *expr.Defuse
}

func newCollect(sctx *super.Context) *collect {
	return &collect{defuse: expr.NewDefuse(sctx)}
}

func (c *collect) NoRip() bool { return true }

func (c *collect) Consume(vec vector.Any) {
	vec = vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		return vecs[0]
	}, c.defuse.Eval(vec))
	if vector.IsDynamic(vec) {
		vec = filterNonesFromDynamic(vec.(*vector.Dynamic))
	}
	if vec.Kind() == vector.KindNone {
		return
	}
	if c.builder == nil {
		c.builder = vbuild.NewDynamicBuilder()
	}
	c.builder.Write(vec)
}

func filterNonesFromDynamic(d *vector.Dynamic) vector.Any {
	var vecs []vector.Any
	newTags := slices.Repeat([]int{-1}, len(d.Values))
	var n uint32
	for i, vec := range d.Values {
		if vec.Kind() == vector.KindNone {
			continue
		}
		newTags[i] = len(vecs)
		vecs = append(vecs, vec)
		n += vec.Len()
	}
	if len(vecs) == len(d.Values) {
		return d
	}
	if n == 0 {
		return vector.NewNone(0)
	}
	index := make([]uint32, n, 0)
	for _, tag := range d.Tags {
		ntag := newTags[tag]
		if ntag == -1 {
			continue
		}
		index = append(index, uint32(ntag))
	}
	return vector.NewDynamic(index, vecs)
}

func (c *collect) consume(vecs ...vector.Any) vector.Any {
	vec := vecs[0]
	if vec.Kind() == vector.KindNone || vec.Len() == 0 {
		return vector.NewNull(vecs[0].Len())
	}
	if c.builder == nil {
		c.builder = vbuild.NewDynamicBuilder()
	}
	c.builder.Write(vec)
	return vector.NewNone(vecs[0].Len())
}

func (c *collect) Result(sctx *super.Context) vector.Any {
	if c.builder == nil {
		atyp := sctx.LookupTypeArray(super.TypeNone)
		return vector.NewArray(atyp, []uint32{0, 0}, vector.NewNone(0))
	}
	vec := c.builder.Build()
	if dynamic, ok := vec.(*vector.Dynamic); ok {
		vec = vector.NewUnionFromDynamic(sctx, dynamic)
	}
	atyp := sctx.LookupTypeArray(vec.Type())
	return vector.NewArray(atyp, []uint32{0, vec.Len()}, vec)
}

func (c *collect) ConsumeAsPartial(partial vector.Any) {
	inner := vector.PushView(partial).(*vector.Array).Values
	c.Consume(vector.Deunion(inner))
}

func (c *collect) ResultAsPartial(sctx *super.Context) vector.Any {
	return c.Result(sctx)
}

// arrayAgg is the same as collect, except if nothing was collected it returns
// null instead of an empty array.
type arrayAgg struct {
	collect
}

func (a *arrayAgg) Result(sctx *super.Context) vector.Any {
	if a.collect.builder == nil {
		return vector.NewNull(1)
	}
	return a.collect.Result(sctx)
}
