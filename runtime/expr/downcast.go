package expr

import (
	"math"
	"slices"

	"github.com/superdb/super"
	samfunc "github.com/superdb/super/runtime/sam/expr/function"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
)

type Downcast struct {
	sctx    *super.Context
	defuser *Defuse
}

func NewDowncast(sctx *super.Context) *Downcast {
	return NewDefuse(sctx).downcast
}

func (d *Downcast) To(from, to vector.Any) vector.Any {
	if to.Kind() != vector.KindType {
		return vector.NewWrappedError(d.sctx, "downcast: type argument not a type", to)
	}
	switch to := to.(type) {
	case *vector.View:
		allTypes := to.Any.(*vector.TypeValue).Types()
		types := make([]super.Type, len(to.Index))
		for i, slot := range to.Index {
			types[i] = allTypes[slot]
		}
		return d.to(from, types)
	case *vector.Dict:
		dictTypes := to.Any.(*vector.TypeValue).Types()
		types := make([]super.Type, len(to.Index))
		for i, slot := range to.Index {
			types[i] = dictTypes[slot]
		}
		return d.to(from, types)
	case *vector.Const:
		typ := vector.TypeValueValue(to, 0)
		return d.downcast(from, typ)
	case *vector.TypeValue:
		return d.to(from, to.Types())
	default:
		panic(to)
	}
}

func (d *Downcast) to(from vector.Any, types []super.Type) vector.Any {
	var indexes [][]uint32
	typeToTag := make(map[super.Type]uint32)
	tags := make([]uint32, len(types))
	for i, typ := range types {
		tag, ok := typeToTag[typ]
		if !ok {
			tag = uint32(len(indexes))
			typeToTag[typ] = tag
			indexes = append(indexes, nil)
		}
		tags[i] = tag
		indexes[tag] = append(indexes[tag], uint32(i))
	}
	if len(indexes) == 1 {
		return d.downcast(from, types[0])
	}
	vals := make([]vector.Any, len(indexes))
	for typ, i := range typeToTag {
		vals[i] = d.downcast(vector.Pick(from, indexes[i]), typ)
	}
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		return vecs[0]
	}, vector.NewDynamic(tags, vals))
}

// downcast converts the input vector vec to the target type "to" presuming
// vec resulted from a fusion having an input type "to".  The expected
// return value is a vector of the same length as vec with type "to".
// If errors are encountered, then the return value is either an error of
// said length or a Dynamic of said length comprised of a valid component
// of type "to" intermixed with one or more other error types.  The caller
// can check for success by comparing the return vector's type with "to".
func (d *Downcast) downcast(vec vector.Any, to super.Type) vector.Any {
	vec = vector.PushView(vec)
	// XXX this shouldn't happen but for some reason fusion vectors
	// show up with dynamics in the fusion.Values fields so this dynamic
	// ends up here.
	if dynamic, ok := vec.(*vector.Dynamic); ok {
		return d.downcastDynamic(dynamic, to)
	}
	if vec.Type() == to {
		return vec
	}
	if vec.Len() == 0 {
		return vector.NewEmpty(to)
	}
	if super.IsTypeAny(vec.Type()) {
		return vector.DefuseAny(vec.(*vector.Fusion))
	}
	if _, ok := to.(*super.TypeUnion); !ok {
		if fusion, ok := vec.(*vector.Fusion); ok {
			return d.downcastFusion(fusion, to)
		}
	}
	if u, ok := vec.(*vector.Union); ok {
		return d.downcastDynamic(u.Dynamic(), to)
	}
	if o, ok := vec.(*vector.Option); ok && !super.IsOptionType(to) {
		return d.downcast(o.Any, to)
	}
	switch to := to.(type) {
	case *super.TypeRecord:
		return d.toRecord(vec, to)
	case *super.TypeArray:
		return d.toArray(vec, to)
	case *super.TypeSet:
		return d.toSet(vec, to)
	case *super.TypeMap:
		return d.toMap(vec, to)
	case *super.TypeUnion:
		return d.toUnion(vec, to)
	case *super.TypeEnum:
		return d.toEnum(vec, to)
	case *super.TypeError:
		return d.toError(vec, to)
	case *super.TypeFusion:
		return vector.NewWrappedError(d.sctx, "downcast: cannot downcast to a fusion type", vec)
	case *super.TypeOption:
		return d.toOption(vec, to)
	default:
		return d.errMismatch(vec, to)
	}
}

func (d *Downcast) downcastFusion(fusion *vector.Fusion, to super.Type) vector.Any {
	vec := d.To(fusion.Values, fusion.Subtypes)
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		vec := vecs[0]
		if vec.Type() != to {
			vec = d.errSubtype(vec, to)
		}
		return vec
	}, vec)
}

func (d *Downcast) downcastDynamic(dynamic *vector.Dynamic, to super.Type) vector.Any {
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		return d.downcast(vecs[0], to)
	}, dynamic)
}

func (d *Downcast) toRecord(vec vector.Any, to *super.TypeRecord) vector.Any {
	rec, ok := vec.(*vector.Record)
	if !ok {
		return d.errMismatch(vec, to)
	}
	if len(to.Fields) == 0 {
		return vector.NewRecord(to, nil, vec.Len())
	}
	var fields []vector.Any
	for _, toField := range to.Fields {
		i, ok := rec.Typ.LUT[toField.Name]
		if !ok {
			return d.errSubtype(vec, to)
		}
		fields = append(fields, d.downcast(rec.Fields[i], toField.Type))
	}
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		for k, vec := range vecs {
			if vec.Type() != to.Fields[k].Type {
				return vec
			}
		}
		return vector.NewRecord(to, vecs, vecs[0].Len())
	}, fields...)
}

func (d *Downcast) toArray(vec vector.Any, to *super.TypeArray) vector.Any {
	array, ok := vec.(*vector.Array)
	if !ok {
		return d.errMismatch(vec, to)
	}
	return d.toContainer(array.Offsets, array.Values, to, to.Type)
}

func (d *Downcast) toSet(vec vector.Any, to *super.TypeSet) vector.Any {
	set, ok := vec.(*vector.Set)
	if !ok {
		return d.errMismatch(vec, to)
	}
	return d.toContainer(set.Offsets, set.Values, to, to.Type)
}

func (d *Downcast) toContainer(offsets []uint32, inner vector.Any, to, toElem super.Type) vector.Any {
	tags, validInner, errVec := d.toList(offsets, inner, toElem)
	if errVec == nil {
		return newContainer(to, offsets, validInner)
	}
	if validInner == nil {
		return errVec
	}
	var offset uint32
	newOffsets := []uint32{offset}
	for i := range uint32(len(offsets) - 1) {
		if tags[i] == 0 {
			offset += offsets[i+1] - offsets[i]
			newOffsets = append(newOffsets, offset)
		}
	}
	vecs := []vector.Any{newContainer(to, newOffsets, validInner), errVec}
	return vector.NewDynamic(tags, vecs)
}

func (d *Downcast) toMap(vec vector.Any, to *super.TypeMap) vector.Any {
	m, ok := vec.(*vector.Map)
	if !ok {
		return d.errMismatch(vec, to)
	}
	keyTags, keys, keyErr := d.toList(m.Offsets, m.Keys, to.KeyType)
	valTags, vals, valErr := d.toList(m.Offsets, m.Values, to.ValType)
	if keyErr == nil && valErr == nil {
		return vector.NewMap(to, m.Offsets, keys, vals)
	}
	if keyTags == nil {
		var tag uint32
		if keys == nil {
			tag = 1
		}
		keyTags = slices.Repeat([]uint32{tag}, int(m.Len()))
	}
	if valTags == nil {
		var tag uint32
		if keys == nil {
			tag = 1
		}
		valTags = slices.Repeat([]uint32{tag}, int(m.Len()))
	}
	var off uint32
	newOffsets := []uint32{off}
	var keyIndex, valIndex []uint32
	var keyCount, keyErrCount, valCount, valErrCount uint32
	var errIndexes [2][]uint32
	tags := make([]uint32, vec.Len())
	for i := range m.Len() {
		offlen := m.Offsets[i+1] - m.Offsets[i]
		keyErr, valErr := keyTags[i] == 1, valTags[i] == 1
		if keyErr || valErr {
			// If err val we need to increment (skip) the key/val counts that
			// are valid so the indexes stay correct for upcoming valid
			// keys/vals.
			var errTag uint32
			var errIndex uint32
			if !valErr {
				valCount += offlen
			} else {
				errTag = 1
				errIndex = valErrCount
				valErrCount++
			}
			if !keyErr {
				keyCount += offlen
			} else {
				errTag = 0
				errIndex = keyErrCount
				keyErrCount++
			}
			tags[i] = errTag + 1
			errIndexes[errTag] = append(errIndexes[errTag], errIndex)
			continue
		}
		for range offlen {
			keyIndex = append(keyIndex, keyCount)
			valIndex = append(valIndex, valCount)
			keyCount++
			valCount++
		}
		off += offlen
		newOffsets = append(newOffsets, off)
	}
	keyErr = vector.Pick(keyErr, errIndexes[0])
	valErr = vector.Pick(valErr, errIndexes[1])
	keys = vector.Pick(keys, keyIndex)
	vals = vector.Pick(vals, valIndex)
	nm := vector.NewMap(to, newOffsets, keys, vals)
	return vector.NewDynamic(tags, []vector.Any{nm, keyErr, valErr})
}

func (d *Downcast) toList(offsets []uint32, vec vector.Any, to super.Type) ([]uint32, vector.Any, vector.Any) {
	n := uint32(len(offsets) - 1)
	vec = d.downcast(vec, to)
	dynamic, ok := vec.(*vector.Dynamic)
	if !ok {
		if vec.Type() != to {
			return nil, nil, vec
		}
		return nil, vec, nil
	}
	innerTags, validVec, errVec := d.separateValidAndErrVecs(dynamic, to)
	if errVec == nil {
		return nil, validVec, nil
	}
	vecs := []vector.Any{validVec, errVec}
	forward := vector.NewDynamic(innerTags, vecs).ForwardTagMap()
	// If a given offset contains a single error value, then the value for that
	// slot is the first error value.
	var indexes [2][]uint32
	tags := make([]uint32, n)
	var tmpslots []uint32
	for i := range n {
		var slot, tag uint32
		tmpslots = tmpslots[:0]
		for slot = offsets[i]; slot < offsets[i+1]; slot++ {
			if innerTags[slot] == 1 {
				tag = 1
				break
			}
			tmpslots = append(tmpslots, forward[slot])
		}
		if tag == 1 {
			indexes[1] = append(indexes[1], forward[slot])
			tags[i] = 1
		} else {
			indexes[0] = append(indexes[0], tmpslots...)
		}
	}
	validVec = vector.Pick(validVec, indexes[0])
	errVec = vector.Pick(errVec, indexes[1])
	return tags, validVec, errVec
}

func (d *Downcast) separateValidAndErrVecs(dynamic *vector.Dynamic, validType super.Type) ([]uint32, vector.Any, vector.Any) {
	errTagMap := slices.Repeat([]uint32{math.MaxUint32}, len(dynamic.Values))
	validTagMap := slices.Clone(errTagMap)
	var validVecs, errVecs []vector.Any
	for i, vec := range dynamic.Values {
		if vec == nil {
			continue
		}
		if vec.Type() != validType {
			if vec.Len() > 0 {
				errTagMap[i] = uint32(len(errVecs))
				errVecs = append(errVecs, vec)
			}
		} else {
			validTagMap[i] = uint32(len(validVecs))
			validVecs = append(validVecs, vec)
		}
	}
	// Combine err vecs together in a single dynamic.
	var validTags, errTags []uint32
	newTags := make([]uint32, len(dynamic.Tags))
	for i, tag := range dynamic.Tags {
		if errTag := errTagMap[tag]; errTag != math.MaxUint32 {
			errTags = append(errTags, errTag)
			newTags[i] = 1
		} else {
			validTags = append(validTags, validTagMap[tag])
		}
	}
	var errs vector.Any
	switch len(errVecs) {
	case 0:
	case 1:
		errs = errVecs[0]
	default:
		errs = vector.NewDynamic(errTags, errVecs)
	}
	var valid vector.Any
	switch len(validVecs) {
	case 0:
	case 1:
		valid = validVecs[0]
	default:
		valid = vbuild.Merge(validTags, validVecs)
	}
	return newTags, valid, errs
}

func newContainer(typ super.Type, offsets []uint32, inner vector.Any) vector.Any {
	switch typ := typ.(type) {
	case *super.TypeArray:
		return vector.NewArray(typ, offsets, inner)
	case *super.TypeSet:
		return vector.NewSet(typ, offsets, inner)
	default:
		panic(typ)
	}
}

func (d *Downcast) toUnion(vec vector.Any, to *super.TypeUnion) vector.Any {
	return d.subTypeOf(vec, to.Types, func(tag int, vec vector.Any) vector.Any {
		if tag < 0 {
			if _, ok := vec.(*vector.Union); ok {
				// Try downcasting the pieces of the union...
				return d.downcast(vec, to)
			}
			return d.errSubtype(vec, to)
		}
		vec = d.downcast(vec, to.Types[tag])
		if dynamic, ok := vec.(*vector.Dynamic); ok {
			for k, vec := range dynamic.Values {
				if vec != nil {
					if vec.Type() == to.Types[tag] {
						dynamic.Values[k] = vector.NewUnionOfOne(to, vec)
					}
				}
			}
			return dynamic
		}
		if vec.Type() != to.Types[tag] {
			return d.errSubtype(vec, to)
		}
		return vector.NewUnionOfOne(to, vec)
	})
}

func (d *Downcast) toEnum(vec vector.Any, to *super.TypeEnum) vector.Any {
	origVec := vec
	var index []uint32
	if view, ok := vec.(*vector.View); ok {
		vec = view.Any
		index = view.Index
	}
	enumVec, ok := vec.(*vector.Enum)
	if !ok {
		return d.errMismatch(origVec, to)
	}
	indexes := make([]uint64, origVec.Len())
	for i := range indexes {
		j := uint32(i)
		if index != nil {
			j = index[j]
		}
		fromIndex := enumVec.Uint.Values[j]
		symbol, err := enumVec.Typ.Symbol(int(fromIndex))
		if err != nil {
			return d.errMismatch(origVec, to)
		}
		toIndex := to.Lookup(symbol)
		if toIndex < 0 {
			return d.errMismatch(origVec, to)
		}
		indexes[i] = uint64(toIndex)
	}
	return vector.NewEnum(to, indexes)
}

func (d *Downcast) toError(vec vector.Any, to *super.TypeError) vector.Any {
	errVec, ok := vec.(*vector.Error)
	if !ok {
		return d.errMismatch(vec, to)
	}
	valsVec := d.downcast(errVec.Vals, to.Type)
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		vec := vecs[0]
		if vec.Type() != to.Type {
			return vec
		}
		return vector.NewError(to, vec)
	}, valsVec)
}

func (d *Downcast) toOption(vec vector.Any, to *super.TypeOption) vector.Any {
	switch vec := vector.Under(vec).(type) {
	case *vector.Option:
		return vector.Apply(vector.ApplyRipOptions, func(vecs ...vector.Any) vector.Any {
			switch vec := vecs[0].(*vector.Option).Any.(type) {
			case *vector.Missing:
				return vector.NewNone(to, vec.Len())
			case *vector.Dynamic:
				panic(vec)
			default:
				vec2 := d.downcast(vec, to.Type)
				if vec2 == nil {
					return nil
				}
				return vector.NewSome(to, vec2)
			}
		}, vec)
	case *vector.View:
		return vector.NewView(d.toOption(vec.Any, to), vec.Index)
	}
	vec = d.downcast(vec, to.Type)
	if vec == nil {
		return nil
	}
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		vec := vecs[0]
		if vec.Type() != to.Type {
			return vec
		}
		return vector.NewSome(to, vec)
	}, vec)
}

func (d *Downcast) subTypeOf(vec vector.Any, types []super.Type, f func(int, vector.Any) vector.Any) vector.Any {
	vec = d.defuser.eval(vec)
	if d, ok := vec.(*vector.Dynamic); ok {
		vals := make([]vector.Any, len(d.Values))
		for i, val := range d.Values {
			if val != nil {
				vals[i] = f(samfunc.DowncastSubtypeIndex(types, val.Type()), val)
			}
		}
		return vector.NewDynamic(d.Tags, vals)
	}
	return f(samfunc.DowncastSubtypeIndex(types, vec.Type()), vec)
}

func (d *Downcast) errNonOptionNone(vec vector.Any, to super.Type) vector.Any {
	return vector.NewStringError(d.sctx, "downcast: none value in non-option type: "+sup.FormatType(to), vec.Len())
}

func (d *Downcast) errMismatch(vec vector.Any, to super.Type) vector.Any {
	return vector.NewWrappedError(d.sctx, "downcast: type mismatch to "+sup.FormatType(to), vec)
}

func (d *Downcast) errSubtype(vec vector.Any, to super.Type) vector.Any {
	return vector.NewWrappedError(d.sctx, "downcast: invalid subtype "+sup.FormatType(to), vec)
}
