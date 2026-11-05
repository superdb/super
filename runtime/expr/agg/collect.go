package agg

import (
	"slices"

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
	groups = orZeros(groups, vec.Len())
	if d, ok := vec.(*vector.Dynamic); ok {
		var index []uint32
		vec, index = filterNonesFromDynamic(d)
		if index != nil {
			groups = pick(groups, index)
		}
		if vec.Len() == 0 {
			return
		}
	}
	c.groups = append(c.groups, groups...)
	c.builder.Write(vec)
}

// filterNonesFromDynamic removes the none values from d.  It returns the
// remaining values and the slots of d they came from, or a nil index if
// nothing was removed.
func filterNonesFromDynamic(d *vector.Dynamic) (vector.Any, []uint32) {
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
		return d, nil
	}
	if n == 0 {
		return vector.NewNone(0), []uint32{}
	}
	tags := make([]uint32, 0, n)
	index := make([]uint32, 0, n)
	for slot, tag := range d.Tags {
		ntag := tagMap[tag]
		if ntag == -1 {
			continue
		}
		tags = append(tags, uint32(ntag))
		index = append(index, uint32(slot))
	}
	return vector.NewDynamic(tags, vecs), index
}

func (c *collect) Result(sctx *super.Context) vector.Any {
	return groupArrays(sctx, c.n, c.groups, c.builder.Build(), c.nullIfEmpty)
}

func (c *collect) ConsumeAsPartial(groups []uint32, partial vector.Any) {
	if partial.Kind() == vector.KindNull {
		return
	}
	elemGroups, values := expandContainer(orZeros(groups, partial.Len()), partial)
	c.Consume(elemGroups, vector.Deunion(values))
}

func (c *collect) ResultAsPartial(sctx *super.Context) vector.Any {
	return groupArrays(sctx, c.n, c.groups, c.builder.Build(), false)
}
