package function

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/expr/cast"
	"github.com/superdb/super/scode"
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
	from, to := vecs[0], vecs[1]
	if from.Kind() == vector.KindError {
		return from
	}
	switch to.Type().ID() {
	case super.IDType:
		return byTarget(from, to, func(from vector.Any, b scode.Bytes) vector.Any {
			typ, err := c.sctx.LookupByValue(b)
			if err != nil {
				panic(err)
			}
			// Casting to any yields fusion(all) values.
			return cast.Cast(c.sctx, from, c.sctx.AnyToFusion(typ))
		})
	case super.IDString:
		return byTarget(from, to, func(from vector.Any, b scode.Bytes) vector.Any {
			return expr.CastToNamed(c.sctx, from, super.DecodeString(b))
		})
	}
	return vector.NewWrappedError(c.sctx, "cast target must be a type or type name", to)
}

// byTarget groups the slots of from by the value of to at each slot and
// calls fn to cast each group to the group's target, which is passed to fn
// as the target's encoding.
func byTarget(from, to vector.Any, fn func(vector.Any, scode.Bytes) vector.Any) vector.Any {
	var b scode.Builder
	if k, ok := to.(*vector.Const); ok {
		k.Serialize(&b, 0)
		return fn(from, b.Bytes().Body())
	}
	groups := map[string]uint32{}
	var targets []string
	var index [][]uint32
	tags := make([]uint32, to.Len())
	for i := range to.Len() {
		b.Truncate()
		to.Serialize(&b, i)
		key := string(b.Bytes().Body())
		tag, ok := groups[key]
		if !ok {
			tag = uint32(len(targets))
			groups[key] = tag
			targets = append(targets, key)
			index = append(index, nil)
		}
		tags[i] = tag
		index[tag] = append(index[tag], i)
	}
	if len(targets) == 1 {
		return fn(from, scode.Bytes(targets[0]))
	}
	outs := make([]vector.Any, len(targets))
	for tag, target := range targets {
		outs[tag] = fn(vector.Pick(from, index[tag]), scode.Bytes(target))
	}
	return vector.Stitch(vector.NewDynamic(tags, outs))
}
