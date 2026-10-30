package op

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Unnest struct {
	sctx   *super.Context
	defuse *expr.SuperDefuse
	parent vio.Puller
	expr   expr.Evaluator
}

func NewUnnest(sctx *super.Context, parent vio.Puller, e expr.Evaluator) *Unnest {
	return &Unnest{
		sctx:   sctx,
		defuse: expr.NewSuperDefuse(sctx),
		parent: parent,
		expr:   e,
	}
}

func (u *Unnest) Pull(done bool) (vector.Any, error) {
	for {
		vec, err := u.parent.Pull(done)
		if vec == nil || err != nil {
			return nil, err
		}
		// deeply deunion
		vec = vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
			return vecs[0]
		}, u.defuse.Eval(u.expr.Eval(vec)))
		if vec, _ = u.flatten(vec); vec != nil {
			return vec, nil
		}
	}
}

func (u *Unnest) flatten(vec vector.Any) (vector.Any, []uint32) {
	if dynamic, ok := vec.(*vector.Dynamic); ok {
		return u.flattenDynamic(dynamic)
	}
	vec = vector.Under(vec)
	switch vec.Kind() {
	case vector.KindNull:
		return nil, nil
	case vector.KindRecord:
		return u.flattenRecord(vector.PushView(vec).(*vector.Record))
	case vector.KindArray:
		array := vector.PushView(vec).(*vector.Array)
		return vector.Deunion(array.Values), array.Offsets
	case vector.KindSet:
		set := vector.PushView(vec).(*vector.Set)
		return vector.Deunion(set.Values), set.Offsets
	default:
		return vector.NewWrappedError(u.sctx, "unnest: encountered non-array value", vec), nil
	}
}

func (u *Unnest) flattenDynamic(vec *vector.Dynamic) (vector.Any, []uint32) {
	vecs := make([]vector.Any, len(vec.Values))
	vecOffsets := make([][]uint32, len(vec.Values))
	for i, vec := range vec.Values {
		if vec == nil {
			continue
		}
		vecs[i], vecOffsets[i] = u.flatten(vec)
	}
	// rebuild tag map
	counts := make([]uint32, len(vec.Values))
	offsets := []uint32{0}
	var length uint32
	var tags []uint32
	for _, tag := range vec.Tags {
		if vecs[tag] == nil {
			continue
		}
		if offsets := vecOffsets[tag]; offsets != nil {
			start := counts[tag]
			for range offsets[start+1] - offsets[start] {
				tags = append(tags, tag)
				length++
			}
			counts[tag]++
		} else {
			tags = append(tags, tag)
			length++
		}
		offsets = append(offsets, length)
	}
	return vector.NewDynamic(tags, vecs), offsets
}

func (u *Unnest) flattenRecord(vec *vector.Record) (vector.Any, []uint32) {
	fields := vec.Fields
	if len(fields) != 2 {
		return vector.NewWrappedError(u.sctx, "unnest: encountered record without two fields", vec), nil
	}
	if union, ok := fields[1].(*vector.Union); ok {
		dynamic := union.Dynamic()
		rtags := dynamic.ReverseTagMap()
		left := fields[0]
		var vals []vector.Any
		for i, right := range dynamic.Values {
			fields := slices.Clone(vec.Typ.Fields)
			fields[1].Type = right.Type()
			typ := u.sctx.MustLookupTypeRecord(fields)
			left := vector.Pick(left, rtags[i])
			vals = append(vals, vector.NewRecord(typ, []vector.Any{left, right}, right.Len()))
		}
		return u.flattenDynamic(vector.NewDynamic(dynamic.Tags, vals))
	}
	right, offsets := u.flatten(fields[1])
	if offsets == nil {
		return vector.NewWrappedError(u.sctx, "unnest: encountered record without an array/set type for second field", vec), nil
	}
	lindex := make([]uint32, 0, right.Len())
	for slot := range vec.Len() {
		for range offsets[slot+1] - offsets[slot] {
			lindex = append(lindex, slot)
		}
	}
	left := vector.Pick(fields[0], lindex)
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		fields := slices.Clone(vec.Typ.Fields)
		fields[1].Type = vecs[1].Type()
		typ := u.sctx.MustLookupTypeRecord(fields)
		return vector.NewRecord(typ, vecs, vecs[0].Len())
	}, left, right), offsets
}
