package function

import (
	"fmt"

	"github.com/superdb/super"
	samfunc "github.com/superdb/super/runtime/sam/expr/function"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
)

type Upcast struct {
	sctx      *super.Context
	typeToTag map[super.Type]uint32
}

func NewUpcast(sctx *super.Context) *Upcast {
	return &Upcast{sctx, map[super.Type]uint32{}}
}

func (u *Upcast) Call(args ...vector.Any) vector.Any {
	from, to := args[0], args[1]
	if to.Kind() != vector.KindType {
		return vector.NewWrappedError(u.sctx, "upcast: type argument not a type", to)
	}
	if to, ok := to.(*vector.Const); ok {
		return u.upcastOrError(from, vector.TypeValueValue(to, 0))
	}
	var indexes [][]uint32
	var tags []uint32
	for i := range to.Len() {
		typ := vector.TypeValueValue(to, i)
		tag, ok := u.typeToTag[typ]
		if !ok {
			tag = uint32(len(u.typeToTag))
			u.typeToTag[typ] = tag
		}
		if len(u.typeToTag) == 1 {
			// There's only one type so we don't need indexes or tags.
			continue
		}
		if len(indexes) == 0 {
			index := make([]uint32, i)
			for i := range i {
				index[i] = i
			}
			indexes = [][]uint32{index}
			tags = make([]uint32, i)
		}
		if !ok {
			indexes = append(indexes, nil)
		}
		indexes[tag] = append(indexes[tag], i)
		tags = append(tags, tag)
	}
	defer clear(u.typeToTag)
	if len(u.typeToTag) == 1 {
		return u.upcastOrError(from, vector.TypeValueValue(to, 0))
	}
	vecs := make([]vector.Any, len(u.typeToTag))
	for typ, tag := range u.typeToTag {
		vecs[tag] = u.upcastOrError(vector.Pick(from, indexes[tag]), typ)
	}
	return vector.NewDynamic(tags, vecs)
}

func (u *Upcast) upcastOrError(vec vector.Any, typ super.Type) vector.Any {
	out := u.upcast(vec, typ)
	if out == nil {
		out = vector.NewWrappedError(u.sctx, "upcast: value not a subtype of "+sup.FormatType(typ), vec)
	}
	return out
}

func (u *Upcast) Cast(vec vector.Any, to super.Type) (vector.Any, bool) {
	out := u.upcast(vec, to)
	return out, out != nil
}

func (u *Upcast) upcast(vec vector.Any, to super.Type) vector.Any {
	if vec.Type() == to && vec.Kind() != vector.KindFusion {
		return vec
	}
	if vec.Len() == 0 {
		return vector.NewEmpty(to)
	}
	switch vec := vec.(type) {
	case *vector.Fusion:
		return u.upcast(vec.Values, to)
	case *vector.View:
		vec2 := u.upcast(vec.Any, to)
		if vec2 == nil {
			return nil
		}
		return vector.Pick(vec2, vec.Index)
	}
	switch to := to.(type) {
	case *super.TypeRecord:
		return u.toRecord(vec, to)
	case *super.TypeArray:
		return u.toArray(vec, to)
	case *super.TypeSet:
		return u.toSet(vec, to)
	case *super.TypeMap:
		return u.toMap(vec, to)
	case *super.TypeUnion:
		return u.toUnion(vec, to)
	case *super.TypeEnum:
		return u.toEnum(vec, to)
	case *super.TypeError:
		return u.toError(vec, to)
	case *super.TypeFusion:
		return u.toFusion(vec, to)
	case *super.TypeOption:
		return u.toOption(vec, to)
	default:
		return nil
	}
}

func (u *Upcast) toRecord(vec vector.Any, to *super.TypeRecord) vector.Any {
	recVec, ok := vec.(*vector.Record)
	if !ok {
		if vec.Kind() == vector.KindUnion {
			return u.deunionAndUpcast(vec, to)
		}
		return nil
	}
	fieldVecs := make([]vector.Any, len(to.Fields))
	for i, f := range to.Fields {
		var vec vector.Any
		n, ok := recVec.Typ.IndexOfField(f.Name)
		if ok {
			vec = recVec.Fields[n]
		} else {
			vec = vector.NewMissing(recVec.Len())
		}
		fieldVecs[i] = u.upcast(vec, f.Type)
		if fieldVecs[i] == nil {
			return nil
		}
	}
	return vector.NewRecord(to, fieldVecs, vec.Len())
}

func (u *Upcast) toArray(vec vector.Any, to *super.TypeArray) vector.Any {
	arrVec, ok := vec.(*vector.Array)
	if !ok {
		return nil
	}
	values := u.deunionAndUpcast(arrVec.Values, to.Type)
	if values == nil {
		return nil
	}
	return vector.NewArray(to, arrVec.Offsets, values)
}

func (u *Upcast) toSet(vec vector.Any, to *super.TypeSet) vector.Any {
	setVec, ok := vec.(*vector.Set)
	if !ok {
		return nil
	}
	values := u.deunionAndUpcast(setVec.Values, to.Type)
	if values == nil {
		return nil
	}
	return vector.NewSet(to, setVec.Offsets, values)
}

func (u *Upcast) deunionAndUpcast(vec vector.Any, to super.Type) vector.Any {
	d, ok := vector.Deunion(vec).(*vector.Dynamic)
	if !ok {
		return u.upcast(vec, to)
	}
	vecs := make([]vector.Any, len(d.Values))
	for i, vec := range d.Values {
		vecs[i] = u.upcast(vec, to)
		if vecs[i] == nil {
			return nil
		}
	}
	// All elements of vecs have type to so this should always return a
	// vector with type to (and never a dynamic).
	return vbuild.MergeSameTypesInDynamic(vector.NewDynamic(d.Tags, vecs))
}

func (u *Upcast) toMap(vec vector.Any, to *super.TypeMap) vector.Any {
	mapVec, ok := vec.(*vector.Map)
	if !ok {
		return nil
	}
	keys := u.deunionAndUpcast(mapVec.Keys, to.KeyType)
	if keys == nil {
		return nil
	}
	values := u.deunionAndUpcast(mapVec.Values, to.ValType)
	if values == nil {
		return nil
	}
	return vector.NewMap(to, mapVec.Offsets, keys, values)
}

func (u *Upcast) toUnion(vec vector.Any, to *super.TypeUnion) vector.Any {
	if unionVec, ok := vec.(*vector.Union); ok {
		types := map[super.Type]struct{}{}
		values := make([]vector.Any, 0, len(to.Types))
		var needMerge bool
		for _, vec := range unionVec.Values() {
			vec = u.toUnionValue(vec, to)
			if vec == nil {
				return nil
			}
			typ := vec.Type()
			if _, ok := types[typ]; !ok {
				needMerge = true
			}
			types[typ] = struct{}{}
			values = append(values, vec)
		}
		tags := unionVec.Tags()
		if needMerge {
			mergedVec := vbuild.MergeSameTypesInDynamic(vector.NewDynamic(tags, values))
			if d, ok := mergedVec.(*vector.Dynamic); ok {
				tags, values = d.Tags, d.Values
			} else {
				tags = make([]uint32, mergedVec.Len())
				values = values[:1]
				values[0] = mergedVec
			}
		}
		for _, typ := range to.Types {
			if _, ok := types[typ]; !ok {
				values = append(values, vector.NewEmpty(typ))
			}
		}
		//XXX We should copy RLE instead of making tags when we can.
		// This will be addressed in a subsequent PR.
		return vector.NewUnion(to, tags, values)
	}
	values := u.toUnionValue(vec, to)
	if values == nil {
		return nil
	}
	return vector.NewUnionOfOne(to, values)
}

func (u *Upcast) toUnionValue(vec vector.Any, to *super.TypeUnion) vector.Any {
	tag := samfunc.UpcastUnionTag(to.Types, vec.Type())
	if tag < 0 {
		return nil
	}
	return u.upcast(vec, to.Types[tag])
}

func (u *Upcast) toEnum(vec vector.Any, to *super.TypeEnum) vector.Any {
	enumVec, ok := vec.(*vector.Enum)
	if !ok {
		return nil
	}
	indexes := make([]uint64, vec.Len())
	for i, fromIndex := range enumVec.Uint.Values {
		symbol, err := enumVec.Typ.Symbol(int(fromIndex))
		if err != nil {
			panic(fmt.Sprintf("bad index %d in %s value", fromIndex, sup.FormatType(enumVec.Typ)))
		}
		toIndex := to.Lookup(symbol)
		if toIndex < 0 {
			return nil
		}
		indexes[i] = uint64(toIndex)
	}
	return vector.NewEnum(to, indexes)
}

func (u *Upcast) toError(vec vector.Any, to *super.TypeError) vector.Any {
	errVec, ok := vec.(*vector.Error)
	if !ok {
		return nil
	}
	values := u.upcast(errVec.Vals, to.Type)
	if values == nil {
		return nil
	}
	return vector.NewError(to, values)
}

func (u *Upcast) toFusion(vec vector.Any, to *super.TypeFusion) vector.Any {
	typ := vec.Type()
	subtypes := make([]super.Type, vec.Len())
	for i := range subtypes {
		subtypes[i] = typ
	}
	var values vector.Any
	if to.Type == super.TypeAll {
		values = vec
	} else {
		values = u.upcast(vec, to.Type)
		if values == nil {
			return nil
		}
	}
	return vector.NewFusion(to, values, subtypes)
}

func (u *Upcast) toOption(vec vector.Any, to *super.TypeOption) vector.Any {
	switch vec := vector.Under(vec).(type) {
	case *vector.Option:
		return vec.Apply(func(tags []uint32, some vector.Any, none *vector.Missing) vector.Any {
			if some == nil {
				return vector.NewNone(to, none.Len())
			}
			some = u.upcast(some, to.Type)
			if some == nil {
				return nil
			}
			if none == nil {
				return vector.NewSome(to, some)
			}
			return vector.NewOption(to, tags, some, none)
		})
	case *vector.Missing:
		return vector.NewNone(to, vec.Len())
	case *vector.View:
		return u.toOption(vector.PushView(vec), to)
	}
	vec = u.upcast(vec, to.Type)
	if vec == nil {
		return nil
	}
	return vector.NewSome(to, vec)
}
