package op

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// Unnest is the unnest operator.  If field isn't empty, the unnested value is
// a record whose field named field is unnested and whose other fields are
// copied to each unnested value.  Otherwise, the unnested value must be an
// array or set.
type Unnest struct {
	sctx   *super.Context
	defuse *expr.SuperDefuse
	parent vio.Puller
	expr   expr.Evaluator
	field  string
}

func NewUnnest(sctx *super.Context, parent vio.Puller, e expr.Evaluator, field string) *Unnest {
	return &Unnest{
		sctx:   sctx,
		defuse: expr.NewSuperDefuse(sctx),
		parent: parent,
		expr:   e,
		field:  field,
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
		if vec, _ = u.flatten(vec, u.field != ""); vec != nil {
			return vec, nil
		}
	}
}

// flatten unnests vec, which is a record whose field named u.field is
// unnested if record is true and otherwise an array or set, and returns the
// unnested values and the offsets of the values of each slot of vec.  It
// returns nil values for nulls and nil offsets for errors, which map
// one-to-one to the slots of vec.
func (u *Unnest) flatten(vec vector.Any, record bool) (vector.Any, []uint32) {
	if dynamic, ok := vec.(*vector.Dynamic); ok {
		return u.flattenDynamic(dynamic, record)
	}
	vec = vector.Under(vec)
	switch vec.Kind() {
	case vector.KindNull:
		return nil, nil
	case vector.KindRecord:
		if record {
			return u.flattenRecord(vector.PushView(vec).(*vector.Record))
		}
	case vector.KindArray:
		array := vector.PushView(vec).(*vector.Array)
		return vector.Deunion(array.Values), array.Offsets
	case vector.KindSet:
		set := vector.PushView(vec).(*vector.Set)
		return vector.Deunion(set.Values), set.Offsets
	}
	return vector.NewWrappedError(u.sctx, "unnest: encountered non-array value", vec), nil
}

func (u *Unnest) flattenDynamic(vec *vector.Dynamic, record bool) (vector.Any, []uint32) {
	vecs := make([]vector.Any, len(vec.Values))
	vecOffsets := make([][]uint32, len(vec.Values))
	for i, vec := range vec.Values {
		if vec == nil {
			continue
		}
		vecs[i], vecOffsets[i] = u.flatten(vec, record)
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

// flattenRecord unnests the field named u.field of each record in vec and
// copies the record's other fields to each unnested value.
func (u *Unnest) flattenRecord(vec *vector.Record) (vector.Any, []uint32) {
	k, ok := vec.Typ.IndexOfField(u.field)
	if !ok {
		// The field is absent since its value is missing.
		return vector.NewWrappedError(u.sctx, "unnest: encountered non-array value", vector.NewMissing(vec.Len())), nil
	}
	if union, ok := vec.Fields[k].(*vector.Union); ok {
		dynamic := union.Dynamic()
		rtags := dynamic.ReverseTagMap()
		vals := make([]vector.Any, 0, len(dynamic.Values))
		for i, val := range dynamic.Values {
			fields := make([]vector.Any, len(vec.Fields))
			for j, f := range vec.Fields {
				if j == k {
					fields[j] = val
				} else {
					fields[j] = vector.Pick(f, rtags[i])
				}
			}
			vals = append(vals, u.newRecord(vec.Typ, fields, k))
		}
		return u.flattenDynamic(vector.NewDynamic(dynamic.Tags, vals), true)
	}
	vals, offsets := u.flatten(vec.Fields[k], false)
	if offsets == nil {
		// vals is nil for nulls or errors for non-array values.
		return vals, nil
	}
	index := make([]uint32, 0, vals.Len())
	for slot := range vec.Len() {
		for range offsets[slot+1] - offsets[slot] {
			index = append(index, slot)
		}
	}
	fields := make([]vector.Any, len(vec.Fields))
	for j, f := range vec.Fields {
		if j == k {
			fields[j] = vals
		} else {
			fields[j] = vector.Pick(f, index)
		}
	}
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		return u.newRecord(vec.Typ, vecs, k)
	}, fields...), offsets
}

// newRecord returns a record vector of the fields of typ whose values are
// vecs, where the type of each field is the type of its value.  k is the
// index of the unnested field, which determines the record's length.
func (u *Unnest) newRecord(typ *super.TypeRecord, vecs []vector.Any, k int) vector.Any {
	fields := slices.Clone(typ.Fields)
	for j, vec := range vecs {
		fields[j].Type = vec.Type()
	}
	return vector.NewRecord(u.sctx.MustLookupTypeRecord(fields), vecs, vecs[k].Len())
}
