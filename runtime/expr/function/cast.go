package function

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/expr/cast"
	"github.com/superdb/super/vector"
)

type caster struct {
	sctx *super.Context
}

func newCaster(sctx *super.Context) expr.Function {
	return &caster{sctx}
}

// Call casts vecs[0] to the targets in vecs[1], which are type values or
// type names (usually a single, constant type).  As with any function, the
// arguments have been defused and Call is applied to each combination of
// argument types.
func (c *caster) Call(vecs ...vector.Any) vector.Any {
	if vec, ok := expr.CatchFirst(c.sctx, expr.CatchAll&^expr.CatchNull, vecs, "cast"); ok {
		return vec
	}
	from, to := vecs[0], vecs[1]
	switch to.Type().ID() {
	case super.IDType:
		return byTarget(from, to, vector.TypeValueValue, func(from vector.Any, typ super.Type) vector.Any {
			// Casting to any yields fusion(all) values.
			return cast.Cast(c.sctx, from, c.sctx.AnyToFusion(typ))
		})
	case super.IDString:
		return byTarget(from, to, vector.StringValue, func(from vector.Any, name string) vector.Any {
			return expr.CastToNamed(c.sctx, from, name)
		})
	}
	return vector.NewWrappedError(c.sctx, "cast target must be a type or type name", to)
}

// byTarget groups the slots of from by the target in to at each slot and
// calls fn to cast each group to its target.
func byTarget[T comparable](from, to vector.Any, at func(vector.Any, uint32) T, fn func(vector.Any, T) vector.Any) vector.Any {
	if _, ok := to.(*vector.Const); ok {
		return fn(from, at(to, 0))
	}
	tagOf := map[T]uint32{}
	var targets []T
	var index [][]uint32
	tags := make([]uint32, to.Len())
	for i := range to.Len() {
		target := at(to, i)
		tag, ok := tagOf[target]
		if !ok {
			tag = uint32(len(targets))
			tagOf[target] = tag
			targets = append(targets, target)
			index = append(index, nil)
		}
		tags[i] = tag
		index[tag] = append(index[tag], i)
	}
	if len(targets) == 1 {
		return fn(from, targets[0])
	}
	outs := make([]vector.Any, len(targets))
	for tag, target := range targets {
		outs[tag] = fn(vector.Pick(from, index[tag]), target)
	}
	return vector.Stitch(vector.NewDynamic(tags, outs))
}
