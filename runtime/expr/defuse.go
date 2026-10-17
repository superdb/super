package expr

import (
	"github.com/superdb/super"
	samfunc "github.com/superdb/super/runtime/sam/expr/function"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
)

type Defuse struct {
	sctx      *super.Context
	downcast  *Downcast
	hasFusion samfunc.FusionChecker
}

func NewDefuse(sctx *super.Context) *Defuse {
	d := &Defuse{
		sctx:     sctx,
		downcast: &Downcast{sctx: sctx},
	}
	d.downcast.defuser = d
	return d
}

func (d *Defuse) Eval(in vector.Any) vector.Any {
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		return d.eval(vecs[0])
	}, in)
}

func (d *Defuse) eval(in vector.Any) vector.Any {
	if !d.hasFusion.Check(in.Type()) {
		return in
	}
	switch in.Kind() {
	case vector.KindRecord:
		return d.defuseRecord(in)
	case vector.KindArray:
		return d.defuseArray(in)
	case vector.KindSet:
		return d.defuseSet(in)
	case vector.KindMap:
		return d.defuseMap(in)
	case vector.KindUnion:
		u := vector.PushView(in).(*vector.Union)
		return d.Eval(u.Dynamic())
	case vector.KindError:
		return d.defuseError(in)
	case vector.KindFusion:
		if in.Len() == 0 {
			return vector.NewMissing(0)
		}
		fusion := vector.PushView(in).(*vector.Fusion)
		if super.IsTypeAny(fusion.Type()) {
			return vector.DefuseAny(fusion)
		}
		return d.downcast.to(fusion.Values, fusion.Subtypes.Types())
	default:
		// primitives, named types, enums
		// BTW, named types are a barrier to defuse.
		return in
	}
}

func (d *Defuse) defuseRecord(vec vector.Any) vector.Any {
	rec := vector.PushView(vec).(*vector.Record)
	var vecs []vector.Any
	for _, vec := range rec.Fields {
		vecs = append(vecs, d.eval(vec))
	}
	// Append length so this still works with empty records.
	vecs = append(vecs, vector.NewNull(rec.Len()))
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		n := vecs[len(vecs)-1].Len()
		vecs = vecs[:len(vecs)-1]
		fieldVecs := vecs[:0]
		var fields []super.Field
		for i, f := range rec.Typ.Fields {
			vec := vecs[i]
			if vec.Kind() == vector.KindMissing {
				continue
			}
			fields = append(fields, super.NewField(f.Name, vec.Type()))
			fieldVecs = append(fieldVecs, vec)
		}
		typ := d.sctx.MustLookupTypeRecord(fields)
		return vector.NewRecord(typ, fieldVecs, n)
	}, vecs...)
}

func (d *Defuse) defuseArray(in vector.Any) vector.Any {
	array := vector.PushView(in).(*vector.Array)
	inner := mergeSameTypes(d.eval(array.Values))
	if !vector.IsDynamic(inner) {
		return vector.NewArray(d.sctx.LookupTypeArray(inner.Type()), array.Offsets, inner)
	}
	tags, inners, offsets := SplitListByTypes(d.sctx, array.Offsets, inner)
	var vals []vector.Any
	for i, inner := range inners {
		typ := d.sctx.LookupTypeArray(inner.Type())
		vals = append(vals, vector.NewArray(typ, offsets[i], inner))
	}
	if len(vals) > 1 {
		return vector.NewDynamic(tags, vals)
	}
	return vals[0]
}

func (d *Defuse) defuseSet(in vector.Any) vector.Any {
	set := vector.PushView(in).(*vector.Set)
	inner := mergeSameTypes(d.eval(set.Values))
	if !vector.IsDynamic(inner) {
		return vector.NewSet(d.sctx.LookupTypeSet(inner.Type()), set.Offsets, inner)
	}
	tags, inners, offsets := SplitListByTypes(d.sctx, set.Offsets, inner)
	var vals []vector.Any
	for i, inner := range inners {
		typ := d.sctx.LookupTypeSet(inner.Type())
		vals = append(vals, vector.NewSet(typ, offsets[i], inner))
	}
	if len(vals) > 1 {
		return vector.NewDynamic(tags, vals)
	}
	return vals[0]
}

func (d *Defuse) defuseMap(in vector.Any) vector.Any {
	vmap := vector.PushView(in).(*vector.Map)
	keys := d.eval(vmap.Keys)
	vals := d.eval(vmap.Values)
	if !vector.IsDynamic(keys) && !vector.IsDynamic(vals) {
		typ := d.sctx.LookupTypeMap(keys.Type(), vals.Type())
		return vector.NewMap(typ, vmap.Offsets, keys, vals)
	}
	keySlotTypes := SlotTypesInList(d.sctx, keys, vmap.Offsets)
	valSlotTypes := SlotTypesInList(d.sctx, vals, vmap.Offsets)
	type mapType struct {
		key super.Type
		val super.Type
	}
	m := make(map[mapType][]uint32)
	for i := range vmap.Len() {
		mtyp := mapType{keySlotTypes[i], valSlotTypes[i]}
		m[mtyp] = append(m[mtyp], uint32(i))
	}
	tags := make([]uint32, len(vmap.Offsets)-1)
	var vecs []vector.Any
	for mtyp, index := range m {
		keys, offsets := SubsetOfList(d.sctx, keys, vmap.Offsets, index, mtyp.key)
		vals, _ := SubsetOfList(d.sctx, vals, vmap.Offsets, index, mtyp.val)
		for _, idx := range index {
			tags[idx] = uint32(len(vecs))
		}
		typ := d.sctx.LookupTypeMap(keys.Type(), vals.Type())
		vecs = append(vecs, vector.NewMap(typ, offsets, keys, vals))
	}
	if len(vecs) == 1 {
		return vecs[0]
	}
	return vector.NewDynamic(tags, vecs)
}

func mergeSameTypes(vec vector.Any) vector.Any {
	if dynamic, ok := vec.(*vector.Dynamic); ok {
		return vbuild.MergeSameTypesInDynamic(dynamic)
	}
	return vec
}

func (d *Defuse) defuseError(in vector.Any) vector.Any {
	errVec := vector.PushView(in).(*vector.Error)
	valsVec := d.eval(errVec.Vals)
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		vec := vecs[0]
		typ := d.sctx.LookupTypeError(vec.Type())
		return vector.NewError(typ, vec)
	}, valsVec)
}
