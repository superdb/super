package expr

import (
	"encoding/binary"

	"github.com/superdb/super"
	samfunc "github.com/superdb/super/runtime/sam/expr/function"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
)

type SuperDefuse struct {
	sctx     *super.Context
	downcast *Downcast
	hasSuper samfunc.SuperChecker
}

func NewSuperDefuse(sctx *super.Context) *SuperDefuse {
	d := &SuperDefuse{
		sctx:     sctx,
		downcast: &Downcast{sctx: sctx},
	}
	d.downcast.defuser = d
	return d
}

func (d *SuperDefuse) Eval(in vector.Any) vector.Any {
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		return d.eval(vecs[0])
	}, in)
}

func (d *SuperDefuse) eval(in vector.Any) vector.Any {
	if !d.hasSuper.Check(in.Type()) {
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
		u := vector.PushViewOrConst(in).(*vector.Union)
		return d.Eval(u.Dynamic())
	case vector.KindError:
		return d.defuseError(in)
	case vector.KindOption:
		return d.defuseOption(in)
	case vector.KindSuper:
		if in.Len() == 0 {
			return vector.NewMissing(0)
		}
		sup := vector.PushViewOrConst(in).(*vector.Super)
		if super.IsSuperAll(sup.Type()) {
			return vector.DefuseAny(sup)
		}
		return d.downcast.to(sup.Values, sup.Subtypes.Types())
	default:
		// primitives, named types, enums
		// BTW, named types are a barrier to defuse.
		return in
	}
}

func (d *SuperDefuse) defuseRecord(vec vector.Any) vector.Any {
	rec := vector.PushViewOrConst(vec).(*vector.Record)
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

// defuseOption defuses the payload of an option.  As for a super value, each
// value gets its own type: a some's payload type is that of its defused value,
// while a none's has each super type replaced by its supertype.
func (d *SuperDefuse) defuseOption(in vector.Any) vector.Any {
	option := vector.UnderOption(in)
	return option.Apply(func(tags []uint32, some vector.Any, none *vector.None) vector.Any {
		var vecs []vector.Any
		var someTags []uint32
		if some != nil {
			some = mergeSameTypes(d.eval(some))
			somes := []vector.Any{some}
			if dynamic, ok := some.(*vector.Dynamic); ok {
				somes, someTags = dynamic.Values, dynamic.Tags
			}
			for _, vec := range somes {
				vecs = append(vecs, vector.NewOptionSome(d.sctx.LookupTypeOption(vec.Type()), vec))
			}
		}
		if none != nil {
			typ := option.Typ
			if t, ok := d.defuseType(typ.Type); ok {
				typ = d.sctx.LookupTypeOption(t)
			}
			vecs = append(vecs, vector.NewOptionNone(typ, none.Len()))
		}
		if len(vecs) == 1 {
			return vecs[0]
		}
		if tags == nil {
			return vector.NewDynamic(someTags, vecs)
		}
		// Map each some to the vector for its type and each none to the last
		// vector.
		outTags := make([]uint32, len(tags))
		var k int
		for i, tag := range tags {
			switch {
			case tag == super.OptionNoneTag:
				outTags[i] = uint32(len(vecs) - 1)
			case someTags != nil:
				outTags[i] = someTags[k]
				k++
			}
		}
		return mergeSameTypes(vector.NewDynamic(outTags, vecs))
	})
}

// defuseType returns typ with each super type replaced by its supertype or
// false if typ contains super(all), whose supertype isn't a type of values.
func (d *SuperDefuse) defuseType(typ super.Type) (super.Type, bool) {
	if !d.hasSuper.Check(typ) {
		return typ, true
	}
	switch typ := typ.(type) {
	case *super.TypeSuper:
		if typ.Type == super.TypeAll {
			return nil, false
		}
		return d.defuseType(typ.Type)
	case *super.TypeRecord:
		fields := make([]super.Field, len(typ.Fields))
		for i, f := range typ.Fields {
			t, ok := d.defuseType(f.Type)
			if !ok {
				return nil, false
			}
			fields[i] = super.NewField(f.Name, t)
		}
		rec, err := d.sctx.LookupTypeRecord(fields)
		return rec, err == nil
	case *super.TypeArray:
		t, ok := d.defuseType(typ.Type)
		if !ok {
			return nil, false
		}
		return d.sctx.LookupTypeArray(t), true
	case *super.TypeSet:
		t, ok := d.defuseType(typ.Type)
		if !ok {
			return nil, false
		}
		return d.sctx.LookupTypeSet(t), true
	case *super.TypeMap:
		key, ok := d.defuseType(typ.KeyType)
		if !ok {
			return nil, false
		}
		val, ok := d.defuseType(typ.ValType)
		if !ok {
			return nil, false
		}
		return d.sctx.LookupTypeMap(key, val), true
	case *super.TypeUnion:
		types := make([]super.Type, 0, len(typ.Types))
		for _, t := range typ.Types {
			t, ok := d.defuseType(t)
			if !ok {
				return nil, false
			}
			types = append(types, t)
		}
		types = super.Flatten(types)
		if len(types) == 1 {
			return types[0], true
		}
		return d.sctx.LookupTypeUnion(types)
	case *super.TypeOption:
		t, ok := d.defuseType(typ.Type)
		if !ok {
			return nil, false
		}
		return d.sctx.LookupTypeOption(t), true
	case *super.TypeError:
		t, ok := d.defuseType(typ.Type)
		if !ok {
			return nil, false
		}
		return d.sctx.LookupTypeError(t), true
	}
	return typ, true
}

func (d *SuperDefuse) defuseArray(in vector.Any) vector.Any {
	array := vector.PushViewOrConst(in).(*vector.Array)
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

func (d *SuperDefuse) defuseSet(in vector.Any) vector.Any {
	set := vector.PushViewOrConst(in).(*vector.Set)
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

func (d *SuperDefuse) defuseMap(in vector.Any) vector.Any {
	vmap := vector.PushViewOrConst(in).(*vector.Map)
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

func (d *SuperDefuse) defuseError(in vector.Any) vector.Any {
	errVec := vector.PushViewOrConst(in).(*vector.Error)
	valsVec := d.eval(errVec.Vals)
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		vec := vecs[0]
		typ := d.sctx.LookupTypeError(vec.Type())
		return vector.NewError(typ, vec)
	}, valsVec)
}

// HasSuper returns true if a value of type typ is changed by Eval.
func (d *SuperDefuse) HasSuper(typ super.Type) bool {
	return d.hasSuper.Check(typ)
}

// Types returns a vector of the types of the values that Eval returns for in.
// Unlike Eval, Types does not access the data of in, which need not be loaded
// (see vector.TypeOnly), except for arrays, sets, and maps whose element
// types have super types.  (The BSUP loader loads the data of these
// containers for type-only projections.)
func (d *SuperDefuse) Types(in vector.Any) vector.Any {
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		return d.types(vecs[0])
	}, in)
}

func (d *SuperDefuse) types(in vector.Any) vector.Any {
	if !d.hasSuper.Check(in.Type()) {
		return vector.NewConstType(d.sctx, in.Type(), in.Len())
	}
	switch in.Kind() {
	case vector.KindRecord:
		return d.recordTypes(in)
	case vector.KindUnion:
		u := vector.PushViewOrConst(in).(*vector.Union)
		return d.Types(u.Dynamic())
	case vector.KindError:
		errVec := vector.PushViewOrConst(in).(*vector.Error)
		vals := d.Types(errVec.Vals)
		types := make([]super.Type, in.Len())
		for i := range in.Len() {
			types[i] = d.sctx.LookupTypeError(vector.TypeValueValue(vals, i))
		}
		return d.typesVector(types)
	case vector.KindSuper:
		return d.typesVector(vector.PushViewOrConst(in).(*vector.Super).Subtypes.Types())
	default:
		// The type of a superdefused array, set, or map depends on the
		// types of its elements so we defuse the data.
		return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
			return vector.NewConstType(d.sctx, vecs[0].Type(), vecs[0].Len())
		}, d.eval(in))
	}
}

func (d *SuperDefuse) recordTypes(in vector.Any) vector.Any {
	rec := vector.PushViewOrConst(in).(*vector.Record)
	vecs := make([]vector.Any, 0, len(rec.Fields))
	for _, vec := range rec.Fields {
		vecs = append(vecs, d.Types(vec))
	}
	// Each distinct combination of field types is looked up once.
	cache := make(map[string]super.Type)
	key := make([]byte, 0, 8*len(vecs))
	fieldTypes := make([]super.Type, len(vecs))
	fields := make([]super.Field, 0, len(vecs))
	types := make([]super.Type, rec.Len())
	for i := range rec.Len() {
		key = key[:0]
		for k, vec := range vecs {
			fieldTypes[k] = vector.TypeValueValue(vec, i)
			key = binary.LittleEndian.AppendUint64(key, uint64(super.TypeID(fieldTypes[k])))
		}
		typ, ok := cache[string(key)]
		if !ok {
			fields = fields[:0]
			for k, f := range rec.Typ.Fields {
				if fieldTypes[k] == super.TypeMissing {
					// See defuseRecord.
					continue
				}
				fields = append(fields, super.NewField(f.Name, fieldTypes[k]))
			}
			typ = d.sctx.MustLookupTypeRecord(fields)
			cache[string(key)] = typ
		}
		types[i] = typ
	}
	return d.typesVector(types)
}

// typesVector returns a vector of type values for types with a constant
// for each distinct type.
func (d *SuperDefuse) typesVector(types []super.Type) vector.Any {
	if len(types) == 0 {
		return vector.NewTypeValueEmpty(d.sctx)
	}
	tagOf := make(map[super.Type]uint32)
	var distinct []super.Type
	tags := make([]uint32, len(types))
	for i, typ := range types {
		tag, ok := tagOf[typ]
		if !ok {
			tag = uint32(len(distinct))
			tagOf[typ] = tag
			distinct = append(distinct, typ)
		}
		tags[i] = tag
	}
	if len(distinct) == 1 {
		return vector.NewConstType(d.sctx, distinct[0], uint32(len(types)))
	}
	counts := make([]uint32, len(distinct))
	for _, tag := range tags {
		counts[tag]++
	}
	vecs := make([]vector.Any, 0, len(distinct))
	for k, typ := range distinct {
		vecs = append(vecs, vector.NewConstType(d.sctx, typ, counts[k]))
	}
	return vector.NewDynamic(tags, vecs)
}
