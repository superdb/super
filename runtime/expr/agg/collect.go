package agg

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
)

type collect struct {
	sctx    *super.Context
	builder *vbuild.DynamicBuilder
	defuse  *expr.SuperDefuse
}

func newCollect(sctx *super.Context) *collect {
	return &collect{sctx: sctx, defuse: expr.NewSuperDefuse(sctx)}
}

func (c *collect) NoRip() bool { return true }

func (c *collect) Consume(vec vector.Any) {
	vec = vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		// catch missing values
		if vec, ok := expr.Catch(c.sctx, 0, vecs[0], "collect"); ok {
			return vec
		}
		return vecs[0]
	}, c.defuse.Eval(vec))
	if d, ok := vec.(*vector.Dynamic); ok {
		vec = filterNonesFromDynamic(d)
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
	tagMap := slices.Repeat([]int{-1}, len(d.Values))
	var n uint32
	for i, vec := range d.Values {
		if vec.Kind() == vector.KindNone {
			continue
		}
		tagMap[i] = len(vecs)
		vecs = append(vecs, vec)
		n += vec.Len()
	}
	if len(vecs) == len(d.Values) {
		return d
	}
	if n == 0 {
		return vector.NewNone(0)
	}
	tags := make([]uint32, 0, n)
	for _, tag := range d.Tags {
		ntag := tagMap[tag]
		if ntag == -1 {
			continue
		}
		tags = append(tags, uint32(ntag))
	}
	return vector.NewDynamic(tags, vecs)
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
