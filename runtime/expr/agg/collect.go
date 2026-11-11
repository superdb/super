package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
)

// collect gathers the values of each group into an array.  Values are
// appended in arrival order along with their groups and are sorted into
// per-group arrays by Result.
type collect struct {
	sctx    *super.Context
	defuse  *expr.SuperDefuse
	n       uint32
	groups  []uint32
	builder *vbuild.DynamicBuilder
	// nullIfEmpty is set for array_agg, which returns null rather than
	// an empty array for a group with no values.
	nullIfEmpty bool
}

func newCollect(sctx *super.Context) *collect {
	return &collect{sctx: sctx, defuse: expr.NewSuperDefuse(sctx), builder: vbuild.NewDynamicBuilder()}
}

func newArrayAgg(sctx *super.Context) *collect {
	c := newCollect(sctx)
	c.nullIfEmpty = true
	return c
}

func (c *collect) NoRip() bool { return true }

func (c *collect) Resize(n uint32) {
	c.n = n
}

func (c *collect) Consume(groups []uint32, vec vector.Any) {
	vec = vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		// catch missing values
		if vec, ok := expr.Catch(c.sctx, 0, vecs[0], "collect"); ok {
			return vec
		}
		return vecs[0]
	}, c.defuse.Eval(vec))
	if vec.Kind() == vector.KindNone {
		return
	}
	if d, ok := vec.(*vector.Dynamic); ok {
		// Drop none values (e.g., from a filter clause), keeping the
		// remaining values in order.
		var index []uint32
		for slot, tag := range d.Tags {
			if d.Values[tag].Kind() != vector.KindNone {
				index = append(index, uint32(slot))
			}
		}
		if len(index) == 0 {
			return
		}
		if len(index) < len(d.Tags) {
			vec, groups = vector.Pick(d, index), pick(groups, index)
		}
	}
	c.groups = append(c.groups, groups...)
	c.builder.Write(vec)
}

func (c *collect) Result(sctx *super.Context) vector.Any {
	return buildArrays(sctx, c.n, c.groups, c.builder.Build(), c.nullIfEmpty)
}

func (c *collect) ConsumeAsPartial(groups []uint32, partial vector.Any) {
	if partial.Kind() == vector.KindNull {
		return
	}
	c.Consume(expandContainer(groups, partial))
}

func (c *collect) ResultAsPartial(sctx *super.Context) vector.Any {
	return buildArrays(sctx, c.n, c.groups, c.builder.Build(), false)
}
