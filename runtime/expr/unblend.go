package expr

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

func Unblend(sctx *super.Context, vec vector.Any) vector.Any {
	switch vec.Kind() {
	case vector.KindRecord:
		return unblendRecord(sctx, vec)
	case vector.KindArray:
		array := vector.PushView(vec).(*vector.Array)
		tags, inners, offsets := SplitListByTypes(sctx, array.Offsets, Unblend(sctx, array.Values))
		var vals []vector.Any
		for i, inner := range inners {
			typ := sctx.LookupTypeArray(inner.Type())
			vals = append(vals, vector.NewArray(typ, offsets[i], inner))
		}
		if len(vals) > 1 {
			return vector.NewDynamic(tags, vals)
		}
		return vals[0]
	case vector.KindSet:
		set := vector.PushView(vec).(*vector.Set)
		tags, inners, offsets := SplitListByTypes(sctx, set.Offsets, Unblend(sctx, set.Values))
		var vals []vector.Any
		for i, inner := range inners {
			typ := sctx.LookupTypeSet(inner.Type())
			vals = append(vals, vector.NewSet(typ, offsets[i], inner))
		}
		if len(vals) > 1 {
			return vector.NewDynamic(tags, vals)
		}
		return vals[0]
	case vector.KindMap:
		return unblendMap(sctx, vector.PushView(vec).(*vector.Map))
	case vector.KindUnion:
		out := vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
			return Unblend(sctx, vecs[0])
		}, vec)
		if dynamic, ok := out.(*vector.Dynamic); ok {
			idx := -1
			// If dynamic only has a single Value of len > 0 return the value.
			for i, val := range dynamic.Values {
				if val != nil && val.Len() > 0 {
					if idx != -1 {
						return out
					}
					idx = i
				}
			}
			return dynamic.Values[idx]
		}
		return out
	}
	return vec
}

func unblendRecord(sctx *super.Context, in vector.Any) vector.Any {
	vec := in
	var index []uint32
	if view, ok := vec.(*vector.View); ok {
		index = view.Index
		vec = view.Any
	}
	rec := vec.(*vector.Record)
	if len(rec.Fields) == 0 {
		return in
	}
	fields := slices.Clone(rec.Fields)
	if index != nil {
		for i, field := range fields {
			fields[i] = vector.Pick(field, index)
		}
	}
	for i, field := range fields {
		fields[i] = Unblend(sctx, field)
	}
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		var fields []super.Field
		var fvecs []vector.Any
		for i, vec := range vecs {
			if _, ok := vec.(*vector.Missing); ok {
				continue
			}
			fvecs = append(fvecs, vec)
			fields = append(fields, super.NewField(rec.Typ.Fields[i].Name, vec.Type()))
		}
		rtyp := sctx.MustLookupTypeRecord(fields)
		return vector.NewRecord(rtyp, fvecs, vecs[0].Len())
	}, fields...)
}

func SplitListByTypes(sctx *super.Context, offsets []uint32, elements vector.Any) ([]uint32, []vector.Any, [][]uint32) {
	dynamic, ok := elements.(*vector.Dynamic)
	if !ok {
		return nil, []vector.Any{elements}, [][]uint32{offsets}
	}
	slotTypes := SlotTypesInList(sctx, dynamic, offsets)
	// Accumulate unique array types.
	m := make(map[super.Type][]uint32)
	for i, typ := range slotTypes {
		m[typ] = append(m[typ], uint32(i))
	}
	dtags := make([]uint32, len(offsets)-1)
	var inners []vector.Any
	var offs [][]uint32
	for typ, index := range m {
		inner, off := SubsetOfList(sctx, dynamic, offsets, index, typ)
		for _, idx := range index {
			dtags[idx] = uint32(len(inners))
		}
		inners = append(inners, inner)
		offs = append(offs, off)
	}
	return dtags, inners, offs
}

func unblendMap(sctx *super.Context, vmap *vector.Map) vector.Any {
	keys := Unblend(sctx, vmap.Keys)
	_, keysAreDynamic := keys.(*vector.Dynamic)
	vals := Unblend(sctx, vmap.Values)
	_, valsAreDynamic := vals.(*vector.Dynamic)
	if !keysAreDynamic && !valsAreDynamic {
		mtyp := sctx.LookupTypeMap(keys.Type(), vals.Type())
		return vector.NewMap(mtyp, vmap.Offsets, keys, vals)
	}
	keySlotTypes := SlotTypesInList(sctx, keys, vmap.Offsets)
	valSlotTypes := SlotTypesInList(sctx, vals, vmap.Offsets)
	type mapType struct {
		key super.Type
		val super.Type
	}
	// Accumulate unique map types.
	m := make(map[mapType][]uint32)
	for i := range vmap.Len() {
		mtyp := mapType{keySlotTypes[i], valSlotTypes[i]}
		m[mtyp] = append(m[mtyp], uint32(i))
	}
	dtags := make([]uint32, len(vmap.Offsets)-1)
	var vecs []vector.Any
	for mtyp, index := range m {
		keys, offsets := SubsetOfList(sctx, keys, vmap.Offsets, index, mtyp.key)
		vals, _ := SubsetOfList(sctx, vals, vmap.Offsets, index, mtyp.val)
		for _, idx := range index {
			dtags[idx] = uint32(len(vecs))
		}
		typ := sctx.LookupTypeMap(keys.Type(), vals.Type())
		vecs = append(vecs, vector.NewMap(typ, offsets, keys, vals))
	}
	if len(vecs) == 1 {
		return vecs[0]
	}
	return vector.NewDynamic(dtags, vecs)
}

func SubsetOfList(sctx *super.Context, elements vector.Any, parentOffsets, index []uint32, typ super.Type) (vector.Any, []uint32) {
	if typ == super.TypeMissing {
		nones := vector.NewMissing(uint32(len(index)))
		offsets := make([]uint32, len(index)+1)
		return nones, offsets
	}
	var allVals []vector.Any
	dynamic, ok := elements.(*vector.Dynamic)
	if ok {
		allVals = dynamic.Values
	} else {
		allVals = []vector.Any{elements}
	}
	var subTypes []super.Type
	utyp, ok := typ.(*super.TypeUnion)
	if ok {
		subTypes = slices.Clone(utyp.Types)
	} else {
		subTypes = append(subTypes, typ)
	}
	subVals := make([]vector.Any, len(subTypes))
	// map parent union tags to subset union tags
	tagMap := make([]uint32, len(allVals))
	for i, typ := range subTypes {
		idx := slices.IndexFunc(allVals, func(vec vector.Any) bool {
			return vec.Type() == typ
		})
		tagMap[idx] = uint32(i)
		subVals[i] = allVals[idx]
	}
	// Generate:
	// - offsets for new array
	// - indexes to create view on values
	// - tags for union (if applicable)
	var forwardTags []uint32
	if dynamic != nil {
		forwardTags = dynamic.ForwardTagMap()
	}
	var tags []uint32
	indexes := make([][]uint32, len(subTypes))
	suboffsets := []uint32{0}
	for _, idx := range index {
		start, end := parentOffsets[idx], parentOffsets[idx+1]
		if dynamic != nil {
			for i, origTag := range dynamic.Tags[start:end] {
				tag := tagMap[origTag]
				tags = append(tags, tag)
				indexes[tag] = append(indexes[tag], forwardTags[start+uint32(i)])
			}
		} else {
			for i := start; i < end; i++ {
				indexes[0] = append(indexes[0], i)
			}
		}
		suboffsets = append(suboffsets, uint32(len(tags)))
	}
	for i, val := range subVals {
		subVals[i] = vector.Pick(val, indexes[i])
	}
	var inner vector.Any
	if len(subVals) > 1 {
		d := vector.FlattenUnions(vector.NewDynamic(tags, subVals))
		inner = vector.NewUnionFromDynamic(sctx, d)
	} else {
		inner = subVals[0]
	}
	return inner, suboffsets
}

func SlotTypesInList(sctx *super.Context, inner vector.Any, offsets []uint32) []super.Type {
	dynamic, _ := vector.Deunion(inner).(*vector.Dynamic)
	var alltypes []super.Type
	if dynamic != nil {
		for _, val := range dynamic.Values {
			alltypes = append(alltypes, val.Type())
		}
	} else {
		alltypes = []super.Type{inner.Type()}
	}
	n := uint32(len(offsets) - 1)
	slotTypes := make([]super.Type, n)
	for i := range n {
		start, end := offsets[i], offsets[i+1]
		if start == end {
			slotTypes[i] = super.TypeMissing
		} else if dynamic != nil {
			slotTypes[i] = typeOfRange(sctx, dynamic, alltypes, start, end)
		} else {
			slotTypes[i] = alltypes[0]
		}
	}
	return slotTypes
}

func typeOfRange(sctx *super.Context, dynamic *vector.Dynamic, alltypes []super.Type, start, end uint32) super.Type {
	tags := slices.Clone(dynamic.Tags[start:end])
	slices.Sort(tags)
	uniq := slices.Compact(tags)
	if len(uniq) == 0 {
		return super.TypeNull
	}
	if len(uniq) == 1 {
		return alltypes[uniq[0]]
	}
	var types []super.Type
	for _, tag := range uniq {
		types = append(types, alltypes[tag])
	}
	return sctx.MustLookupTypeUnion(types)
}
