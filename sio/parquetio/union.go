package parquetio

import (
	"bytes"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/arrowio"
	"github.com/superdb/super/sio/supio"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

// unionTypeKey is the key of the Parquet key-value metadata that holds the
// SUP type of the values written when the type has a union, which Parquet
// can't represent so the writer encodes it as a struct with a field per
// member (see arrowio.Writer.UnionsAsStructs).  The reader uses the type to
// decode the structs back into unions.
const unionTypeKey = "super.type"

// needsType returns true if the reader needs typ to decode values of typ,
// i.e., if typ has a union that the writer encodes as a struct (a union
// other than a nullable union like T|null, which is a nullable column) or a
// record field that the writer drops because its type is an empty record.
func needsType(typ super.Type) bool {
	switch typ := typ.(type) {
	case *super.TypeNamed:
		return needsType(typ.Type)
	case *super.TypeRecord:
		for _, f := range typ.Fields {
			if arrowio.IsEmptyRecord(f.Type) || needsType(f.Type) {
				return true
			}
		}
	case *super.TypeArray:
		return needsType(typ.Type)
	case *super.TypeSet:
		return needsType(typ.Type)
	case *super.TypeMap:
		return needsType(typ.KeyType) || needsType(typ.ValType)
	case *super.TypeOption:
		return needsType(typ.Type)
	case *super.TypeError:
		return needsType(typ.Type)
	case *super.TypeUnion:
		if t := nullableUnionType(typ); t != nil {
			return needsType(t)
		}
		return true
	}
	return false
}

// nullableUnionType returns T if u is a nullable union, i.e., a union of T
// and any of null, missing, none, and option types, as for
// arrowio.Writer.newArrowField, and nil otherwise.
func nullableUnionType(u *super.TypeUnion) super.Type {
	var out super.Type
	for _, typ := range u.Types {
		if isNullType(typ) || super.IsOptionType(typ) {
			continue
		}
		if out != nil {
			return nil
		}
		out = typ
	}
	return out
}

func isNullType(typ super.Type) bool {
	return typ == super.TypeNull || typ == super.TypeMissing || typ == super.TypeNone
}

// formatType returns typ as SUP text: the declarations of its named types
// followed by its type value.
func formatType(typ super.Type) (string, error) {
	var b bytes.Buffer
	w := supio.NewWriter(sio.NopCloser(&b), supio.WriterOpts{ColorDisabled: true})
	if err := w.Write(super.NewTypeValue(typ)); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return strings.TrimSpace(b.String()), nil
}

// parseType parses the SUP text of formatType.
func parseType(sctx *super.Context, text string) (super.Type, error) {
	val, err := supio.NewReader(sctx, strings.NewReader(text)).Read()
	if err != nil {
		return nil, err
	}
	if val == nil || val.Type() != super.TypeType {
		return nil, errors.New("not a type value")
	}
	return sctx.LookupByValue(val.Bytes())
}

// unionMembers returns the index of the struct field for each member of
// union u as the writer encodes it, with -1 for the null, missing, and none
// members, which have no field, and the number of fields.
func unionMembers(u *super.TypeUnion) ([]int, int) {
	fields := make([]int, len(u.Types))
	var n int
	for k, typ := range u.Types {
		if isNullType(typ) {
			fields[k] = -1
			continue
		}
		fields[k] = n
		n++
	}
	return fields, n
}

// typedType returns the type of the vector that buildTyped builds from an
// arrow array of type dt for a value whose type as written is want.  It
// follows want where dt has the structure that the writer gives want and
// otherwise returns the type inferred from dt.
func (v *vectorBuilder) typedType(dt arrow.DataType, nullable bool, want super.Type) (super.Type, error) {
	if isNullType(want) && dt.ID() == arrow.NULL {
		// The writer writes null, missing, and none as an arrow null.
		return want, nil
	}
	if arrowio.IsEmptyRecord(want) && dt.ID() == arrow.BOOL {
		// The writer writes an empty record union member as a boolean.
		return want, nil
	}
	switch want := want.(type) {
	case *super.TypeOfIP, *super.TypeOfNet, *super.TypeOfType, *super.TypeEnum, *super.TypeError:
		// The writer writes these as strings.
		if !nullable && dt.ID() == arrow.STRING {
			return want, nil
		}
	case *super.TypeNamed:
		typ, err := v.typedType(dt, nullable, want.Type)
		if err != nil || typ != want.Type {
			return typ, err
		}
		return want, nil
	case *super.TypeUnion:
		if t := nullableUnionType(want); t != nil {
			if !nullable {
				break
			}
			typ, err := v.typedType(dt, false, t)
			if err != nil {
				return nil, err
			}
			if typ == t && !hasOptionMember(want) {
				return want, nil
			}
			if u, ok := v.sctx.LookupTypeUnion([]super.Type{typ, super.TypeNull}); ok {
				return u, nil
			}
			break
		}
		st, ok := dt.(*arrow.StructType)
		if !ok {
			break
		}
		fields, n := unionMembers(want)
		if st.NumFields() != n {
			break
		}
		types := make([]super.Type, len(want.Types))
		for k, typ := range want.Types {
			if fields[k] < 0 {
				types[k] = typ
				continue
			}
			var err error
			if types[k], err = v.typedType(st.Field(fields[k]).Type, false, typ); err != nil {
				return nil, err
			}
		}
		if hasDuplicateTypes(types) {
			break
		}
		if u, ok := v.sctx.LookupTypeUnion(types); ok {
			return u, nil
		}
	case *super.TypeOption:
		// The writer writes an option as a nullable column.
		if !nullable {
			break
		}
		typ, err := v.typedType(dt, false, want.Type)
		if err != nil {
			return nil, err
		}
		return v.sctx.LookupTypeOption(typ), nil
	case *super.TypeRecord:
		st, ok := dt.(*arrow.StructType)
		if !ok || nullable {
			break
		}
		fields := make([]super.Field, len(want.Fields))
		var i int
		for k, f := range want.Fields {
			if arrowio.IsEmptyRecord(f.Type) {
				// The writer drops these fields.
				fields[k] = f
				continue
			}
			if i >= st.NumFields() || st.Field(i).Name != f.Name {
				return v.inferredType(dt, nullable)
			}
			typ, err := v.typedType(st.Field(i).Type, st.Field(i).Nullable, f.Type)
			if err != nil {
				return nil, err
			}
			fields[k] = super.NewField(f.Name, typ)
			i++
		}
		if i != st.NumFields() {
			return v.inferredType(dt, nullable)
		}
		return v.sctx.LookupTypeRecord(fields)
	case *super.TypeArray, *super.TypeSet:
		lt, ok := dt.(*arrow.ListType)
		if !ok || nullable {
			break
		}
		elem, err := v.typedType(lt.Elem(), lt.ElemField().Nullable, super.InnerType(want))
		if err != nil {
			return nil, err
		}
		if _, ok := want.(*super.TypeSet); ok {
			return v.sctx.LookupTypeSet(elem), nil
		}
		return v.sctx.LookupTypeArray(elem), nil
	case *super.TypeMap:
		mt, ok := dt.(*arrow.MapType)
		if !ok || nullable {
			break
		}
		key, err := v.typedType(mt.KeyType(), mt.KeyField().Nullable, want.KeyType)
		if err != nil {
			return nil, err
		}
		val, err := v.typedType(mt.ItemType(), mt.ItemField().Nullable, want.ValType)
		if err != nil {
			return nil, err
		}
		return v.sctx.LookupTypeMap(key, val), nil
	}
	return v.inferredType(dt, nullable)
}

// inferredType returns the type that build infers from an arrow array of
// type dt.
func (v *vectorBuilder) inferredType(dt arrow.DataType, nullable bool) (super.Type, error) {
	typ, err := arrowio.NewTypeFromSchema(v.sctx, arrow.NewSchema([]arrow.Field{{Name: "x", Type: dt, Nullable: nullable}}, nil))
	if err != nil {
		return nil, err
	}
	return typ.(*super.TypeRecord).Fields[0].Type, nil
}

func hasDuplicateTypes(types []super.Type) bool {
	seen := make(map[super.Type]struct{}, len(types))
	for _, typ := range types {
		if _, ok := seen[typ]; ok {
			return true
		}
		seen[typ] = struct{}{}
	}
	return false
}

// buildTyped builds a vector of type typedType(a.DataType(), nullable, want)
// from a.
func (v *vectorBuilder) buildTyped(a arrow.Array, nullable bool, want super.Type) (vector.Any, error) {
	typ, err := v.typedType(a.DataType(), nullable, want)
	if err != nil {
		return nil, err
	}
	if isNullType(typ) {
		return nullVector(typ, uint32(a.Len())), nil
	}
	if typ == want && arrowio.IsEmptyRecord(want) && a.DataType().ID() == arrow.BOOL {
		return emptyRecordVector(want, uint32(a.Len())), nil
	}
	switch want := want.(type) {
	case *super.TypeOfIP, *super.TypeOfNet, *super.TypeOfType, *super.TypeEnum, *super.TypeError:
		if typ == want {
			return v.buildFromStrings(a.(*array.String), want)
		}
	case *super.TypeNamed:
		vec, err := v.buildTyped(a, nullable, want.Type)
		if err != nil || typ != want {
			return vec, err
		}
		return vector.NewNamed(want, vec), nil
	case *super.TypeUnion:
		if !isUnionOf(typ, want) {
			break
		}
		if t := nullableUnionType(want); t != nil {
			vec, err := v.buildTyped(a, false, t)
			if err != nil {
				return nil, err
			}
			return buildUnionWithNulls(typ.(*super.TypeUnion), vec, a), nil
		}
		return v.buildUnion(a.(*array.Struct), want, typ.(*super.TypeUnion))
	case *super.TypeOption:
		optType, ok := typ.(*super.TypeOption)
		if !ok {
			break
		}
		vec, err := v.buildTyped(a, false, want.Type)
		if err != nil {
			return nil, err
		}
		return buildOption(optType, vec, a), nil
	case *super.TypeRecord:
		recType, ok := typ.(*super.TypeRecord)
		if !ok || !isRecordOf(recType, want) {
			break
		}
		arr := a.(*array.Struct)
		st := arr.DataType().(*arrow.StructType)
		fields := make([]vector.Any, len(want.Fields))
		var i int
		for k, f := range want.Fields {
			if arrowio.IsEmptyRecord(f.Type) {
				fields[k] = emptyRecordVector(f.Type, uint32(a.Len()))
				continue
			}
			if fields[k], err = v.buildTyped(arr.Field(i), st.Field(i).Nullable, f.Type); err != nil {
				return nil, err
			}
			i++
		}
		return vector.NewRecord(recType, fields, uint32(a.Len())), nil
	case *super.TypeArray, *super.TypeSet:
		if super.TypeUnder(typ).Kind() != want.Kind() {
			break
		}
		arr := a.(*array.List)
		elems, err := v.buildTyped(arr.ListValues(), arr.DataType().(*arrow.ListType).ElemField().Nullable, super.InnerType(want))
		if err != nil {
			return nil, err
		}
		offsets := listOffsets(arr)
		if setType, ok := typ.(*super.TypeSet); ok {
			return vector.NewSet(setType, offsets, elems), nil
		}
		return vector.NewArray(typ.(*super.TypeArray), offsets, elems), nil
	case *super.TypeMap:
		mapType, ok := typ.(*super.TypeMap)
		if !ok {
			break
		}
		arr := a.(*array.Map)
		mt := arr.DataType().(*arrow.MapType)
		keys, err := v.buildTyped(arr.Keys(), mt.KeyField().Nullable, want.KeyType)
		if err != nil {
			return nil, err
		}
		vals, err := v.buildTyped(arr.Items(), mt.ItemField().Nullable, want.ValType)
		if err != nil {
			return nil, err
		}
		return vector.NewMap(mapType, listOffsets(arr), keys, vals), nil
	}
	return v.build(a, nullable)
}

// isUnionOf returns true if typedType followed union want.
func isUnionOf(typ super.Type, want *super.TypeUnion) bool {
	u, ok := typ.(*super.TypeUnion)
	if !ok {
		return false
	}
	if nullableUnionType(want) != nil {
		return nullableUnionType(u) != nil
	}
	return len(u.Types) == len(want.Types)
}

// isRecordOf returns true if typedType followed record want.
func isRecordOf(typ, want *super.TypeRecord) bool {
	if len(typ.Fields) != len(want.Fields) {
		return false
	}
	for i, f := range want.Fields {
		if typ.Fields[i].Name != f.Name {
			return false
		}
	}
	return true
}

func listOffsets(a interface{ Offsets() []int32 }) []uint32 {
	offsets := make([]uint32, len(a.Offsets()))
	for i, o := range a.Offsets() {
		offsets[i] = uint32(o)
	}
	return offsets
}

// buildUnion decodes struct a, which encodes union want, as a union of type
// typ (see typedType).  Each value is the member whose field is non-null or,
// if none is (or the struct is null), the null, missing, or none member.
// The member vectors are built from the whole fields of a and then picked
// since arrow arrays can't be indexed.
func (v *vectorBuilder) buildUnion(a *array.Struct, want, typ *super.TypeUnion) (vector.Any, error) {
	fields, _ := unionMembers(want)
	nullMember := -1
	for k, f := range fields {
		if f < 0 {
			nullMember = k
			break
		}
	}
	n := a.Len()
	members := make([]uint32, n)
	indexes := make([][]uint32, len(want.Types))
	for i := range n {
		member := nullMember
		if !a.IsNull(i) {
			for k, f := range fields {
				if f >= 0 && a.Field(f).IsValid(i) {
					member = k
					break
				}
			}
		}
		if member < 0 {
			// A union in a value of another member of an enclosing
			// union is null.  Its member doesn't matter since the
			// enclosing union drops the value.
			member = 0
		}
		members[i] = uint32(member)
		indexes[member] = append(indexes[member], uint32(i))
	}
	vecs := make([]vector.Any, len(typ.Types))
	tagOf := make([]uint32, len(want.Types))
	for k, memberType := range want.Types {
		var vec vector.Any
		if f := fields[k]; f >= 0 {
			full, err := v.buildTyped(a.Field(f), false, memberType)
			if err != nil {
				return nil, err
			}
			vec = vector.Pick(full, indexes[k])
		} else {
			vec = nullVector(memberType, uint32(len(indexes[k])))
		}
		tag := typ.TagOf(vec.Type())
		if tag < 0 {
			return nil, fmt.Errorf("Parquet union member %s not in %s", sup.FormatType(vec.Type()), sup.FormatType(typ))
		}
		tagOf[k] = uint32(tag)
		vecs[tag] = vec
	}
	tags := make([]uint32, n)
	for i, member := range members {
		tags[i] = tagOf[member]
	}
	return vector.NewUnion(typ, tags, vecs), nil
}

// buildOption returns an option vector of type typ whose values are none
// where a is null and otherwise the values of vec.
func buildOption(typ *super.TypeOption, vec vector.Any, a arrow.Array) vector.Any {
	tags := make([]uint32, a.Len())
	var some []uint32
	var nones uint32
	for i := range tags {
		if a.IsNull(i) {
			tags[i] = super.OptionNoneTag
			nones++
		} else {
			tags[i] = super.OptionSomeTag
			some = append(some, uint32(i))
		}
	}
	return vector.NewOption(typ, tags, vector.Pick(vec, some), vector.NewNone(nones))
}

func hasOptionMember(u *super.TypeUnion) bool {
	for _, typ := range u.Types {
		if super.IsOptionType(typ) {
			return true
		}
	}
	return false
}

// buildUnionWithNulls builds a vector of union typ, which has one member
// other than null, missing, and none, from vec, which holds the values of
// that member, and a, whose nulls are the null member (or the missing or none
// member if typ has no null).
func buildUnionWithNulls(typ *super.TypeUnion, vec vector.Any, a arrow.Array) vector.Any {
	nullType := super.Type(super.TypeNull)
	if typ.TagOf(nullType) < 0 {
		for _, t := range typ.Types {
			if isNullType(t) {
				nullType = t
				break
			}
		}
	}
	nullTag, vecTag := uint32(typ.TagOf(nullType)), uint32(typ.TagOf(vec.Type()))
	tags := make([]uint32, a.Len())
	var some []uint32
	for i := range tags {
		if a.IsNull(i) {
			tags[i] = nullTag
		} else {
			tags[i] = vecTag
			some = append(some, uint32(i))
		}
	}
	vecs := make([]vector.Any, len(typ.Types))
	for k, t := range typ.Types {
		vecs[k] = nullVector(t, 0)
	}
	vecs[nullTag] = nullVector(nullType, uint32(a.Len()-len(some)))
	vecs[vecTag] = vector.Pick(vec, some)
	return vector.NewUnion(typ, tags, vecs)
}

// buildFromStrings decodes a, which holds values of type typ formatted by
// the writer, as a vector of type typ.
func (v *vectorBuilder) buildFromStrings(a *array.String, typ super.Type) (vector.Any, error) {
	b := vector.NewValueBuilder(typ)
	var placeholder scode.Bytes
	switch typ.(type) {
	case *super.TypeOfIP:
		placeholder = super.EncodeIP(netip.IPv4Unspecified())
	case *super.TypeOfNet:
		placeholder = super.EncodeNet(netip.PrefixFrom(netip.IPv4Unspecified(), 0))
	case *super.TypeOfType:
		placeholder = super.EncodeTypeValue(super.TypeNull)
	case *super.TypeEnum:
		placeholder = super.EncodeUint(0)
	case *super.TypeError:
		// Use the first value for nulls.
		for i := range a.Len() {
			if a.IsValid(i) {
				val, err := sup.ParseValue(v.sctx, a.Value(i))
				if err != nil {
					return nil, err
				}
				placeholder = val.Bytes()
				break
			}
		}
	}
	for i := range a.Len() {
		if a.IsNull(i) {
			// The value isn't used (e.g., it's in an unused union member).
			b.Write(placeholder)
			continue
		}
		bytes, err := v.decodeString(a.Value(i), typ)
		if err != nil {
			return nil, err
		}
		b.Write(bytes)
	}
	return b.Build(v.sctx), nil
}

func (v *vectorBuilder) decodeString(s string, typ super.Type) (scode.Bytes, error) {
	switch typ := typ.(type) {
	case *super.TypeOfIP:
		a, err := netip.ParseAddr(s)
		return super.EncodeIP(a), err
	case *super.TypeOfNet:
		p, err := netip.ParsePrefix(s)
		return super.EncodeNet(p), err
	case *super.TypeEnum:
		k := typ.Lookup(s)
		if k < 0 {
			return nil, fmt.Errorf("%q is not a symbol of %s", s, sup.FormatType(typ))
		}
		return super.EncodeUint(uint64(k)), nil
	default:
		val, err := sup.ParseValue(v.sctx, s)
		if err != nil {
			return nil, err
		}
		if val.Type() != typ {
			return nil, fmt.Errorf("%s is not of type %s", s, sup.FormatType(typ))
		}
		return val.Bytes(), nil
	}
}

// emptyRecordVector returns a vector of n values of typ, which is an empty
// record (see arrowio.IsEmptyRecord).
func emptyRecordVector(typ super.Type, n uint32) vector.Any {
	if named, ok := typ.(*super.TypeNamed); ok {
		return vector.NewNamed(named, emptyRecordVector(named.Type, n))
	}
	rec := typ.(*super.TypeRecord)
	fields := make([]vector.Any, len(rec.Fields))
	for i, f := range rec.Fields {
		fields[i] = emptyRecordVector(f.Type, n)
	}
	return vector.NewRecord(rec, fields, n)
}

func nullVector(typ super.Type, n uint32) vector.Any {
	switch typ {
	case super.TypeMissing:
		return vector.NewMissing(n)
	case super.TypeNone:
		return vector.NewNone(n)
	}
	return vector.NewNull(n)
}
