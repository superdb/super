package function

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sup"
)

type downcast struct {
	sctx    *super.Context
	defuser *Defuse
}

func (d *downcast) Call(args []super.Value) super.Value {
	from, to := args[0], args[1]
	if _, ok := super.TypeUnder(to.Type()).(*super.TypeOfType); !ok {
		return d.sctx.WrapError("downcast: type argument not a type", to)
	}
	typ, err := d.sctx.LookupByValue(to.Bytes())
	if err != nil {
		panic(err)
	}
	val, errVal := d.downcast(from.Type(), from.Bytes(), typ)
	if errVal != nil {
		return *errVal
	}
	return val
}

func (d *downcast) Cast(from super.Value, to super.Type) (super.Value, bool) {
	val, errVal := d.downcast(from.Type(), from.Bytes(), to)
	return val, errVal == nil
}

func (d *downcast) downcast(typ super.Type, bytes scode.Bytes, to super.Type) (super.Value, *super.Value) {
	if typ == super.TypeAll {
		return super.NewValue(to, bytes), nil
	}
	if _, ok := to.(*super.TypeUnion); !ok {
		if fusionType, ok := typ.(*super.TypeFusion); ok {
			superBytes, subtype := fusionType.Deref(d.sctx, bytes)
			if subtype != to {
				val, errVal := d.defuse(fusionType, bytes)
				if errVal != nil {
					panic(errVal)
				}
				return super.Value{}, d.errSubtype(val.Type(), val.Bytes(), to)
			}
			return d.downcast(fusionType.Type, superBytes, subtype)
		}
	}
	typ, bytes = deunion(typ, bytes)
	if o, ok := typ.(*super.TypeOption); ok && !super.IsOptionType(to) {
		typ, bytes := o.Decode(bytes)
		return d.downcast(typ, bytes, to)
	}
	switch to := to.(type) {
	case *super.TypeRecord:
		return d.toRecord(typ, bytes, to)
	case *super.TypeArray:
		return d.toArray(typ, bytes, to)
	case *super.TypeSet:
		return d.toSet(typ, bytes, to)
	case *super.TypeMap:
		return d.toMap(typ, bytes, to)
	case *super.TypeUnion:
		return d.toUnion(typ, bytes, to)
	case *super.TypeOption:
		return d.toOption(typ, bytes, to)
	case *super.TypeEnum:
		return d.toEnum(typ, bytes, to)
	case *super.TypeError:
		return d.toError(typ, bytes, to)
	case *super.TypeFusion:
		// Can't downcast to a super type
		return super.Value{}, d.sctx.WrapError("downcast: cannot downcast to a fusion type", super.NewValue(typ, bytes)).Ptr()
	default:
		if typ == to {
			return super.NewValue(typ, bytes), nil
		} else {
			typ, bytes := deunion(typ, bytes)
			if typ == to {
				return super.NewValue(typ, bytes), nil
			}
		}
		if typ == super.TypeMissing {
			return super.Value{}, d.errMissing(to)
		}
		return super.Value{}, d.errMismatch(typ, bytes, to)
	}
}

func (d *downcast) defuse(fusionType *super.TypeFusion, bytes scode.Bytes) (super.Value, *super.Value) {
	superBytes, subtype := fusionType.Deref(d.sctx, bytes)
	return d.downcast(fusionType.Type, superBytes, subtype)
}

func (d *downcast) toRecord(typ super.Type, bytes scode.Bytes, to *super.TypeRecord) (super.Value, *super.Value) {
	fromType, ok := typ.(*super.TypeRecord)
	if !ok {
		return super.Value{}, d.errMismatch(typ, bytes, to)
	}
	b := scode.NewBuilder()
	b.BeginContainer()
	for _, toField := range to.Fields { // ranging through to fields and lookup up from
		elemType, elemBytes, ok := derefAsBytes(fromType, bytes, toField.Name)
		if !ok {
			// The super value must have all the fields of the subtype cast.
			// It's missing a field, so fail.
			return super.Value{}, d.errSubtype(typ, bytes, to)
		}
		if super.IsOptionType(toField.Type) {
			fromFieldType := elemType
			if f, ok := fromFieldType.(*super.TypeFusion); ok {
				fromFieldType = f.Type
			}
			if !super.IsOptionType(fromFieldType) {
				return super.Value{}, d.errSubtype(typ, bytes, to)
			}
		}
		// We have the value and the to field.  Downcast recursively.
		val, errVal := d.downcast(elemType, elemBytes, toField.Type)
		if errVal != nil {
			return super.Value{}, errVal
		}
		b.Append(val.Bytes())
	}
	b.EndContainer()
	return super.NewValue(to, b.Bytes().Body()), nil
}

func (d *downcast) toArray(typ super.Type, bytes scode.Bytes, to *super.TypeArray) (super.Value, *super.Value) {
	if arrayType, ok := typ.(*super.TypeArray); ok {
		return d.toContainer(arrayType.Type, bytes, to, to.Type)
	}
	return super.Value{}, d.errMismatch(typ, bytes, to)
}

func (d *downcast) toSet(typ super.Type, bytes scode.Bytes, to *super.TypeSet) (super.Value, *super.Value) {
	if setType, ok := typ.(*super.TypeSet); ok {
		// XXX normalize set contents? can reach into body here blah
		return d.toContainer(setType.Type, bytes, to, to.Type)
	}
	return super.Value{}, d.errMismatch(typ, bytes, to)
}

func (d *downcast) toContainer(elemType super.Type, bytes scode.Bytes, to super.Type, toElem super.Type) (super.Value, *super.Value) {
	b := scode.NewBuilder()
	b.BeginContainer()
	for it := bytes.Iter(); !it.Done(); {
		val, errVal := d.downcast(elemType, it.Next(), toElem)
		if errVal != nil {
			return super.Value{}, errVal
		}
		b.Append(val.Bytes())
	}
	b.EndContainer()
	return super.NewValue(to, b.Bytes().Body()), nil
}

func (d *downcast) toMap(typ super.Type, bytes scode.Bytes, to *super.TypeMap) (super.Value, *super.Value) {
	mapType, ok := typ.(*super.TypeMap)
	if !ok {
		return super.Value{}, d.errMismatch(typ, bytes, to)
	}
	b := scode.NewBuilder()
	b.BeginContainer()
	for it := bytes.Iter(); !it.Done(); {
		key, errVal := d.downcast(mapType.KeyType, it.Next(), to.KeyType)
		if errVal != nil {
			return super.Value{}, errVal
		}
		b.Append(key.Bytes())
		val, errVal := d.downcast(mapType.ValType, it.Next(), to.ValType)
		if errVal != nil {
			return super.Value{}, errVal
		}
		b.Append(val.Bytes())
	}
	b.EndContainer()
	return super.NewValue(to, b.Bytes().Body()), nil
}

func (d *downcast) toUnion(typ super.Type, bytes scode.Bytes, to *super.TypeUnion) (super.Value, *super.Value) {
	if typ == to {
		return super.NewValue(typ, bytes), nil
	}
	tag, typ, bytes := d.subTypeOf(typ, bytes, to.Types)
	if tag < 0 {
		if _, ok := typ.(*super.TypeUnion); ok {
			typ, bytes = deunion(typ, bytes)
			return d.downcast(typ, bytes, to)
		}
		return super.Value{}, d.errSubtype(typ, bytes, to)
	}
	val, errVal := d.downcast(typ, bytes, to.Types[tag])
	if errVal != nil {
		return super.Value{}, errVal
	}
	b := scode.NewBuilder()
	super.BeginUnion(b, tag)
	b.Append(val.Bytes())
	b.EndContainer()
	return super.NewValue(to, b.Bytes().Body()), nil
}

func (d *downcast) toOption(typ super.Type, bytes scode.Bytes, to *super.TypeOption) (super.Value, *super.Value) {
	if optionType, ok := typ.(*super.TypeOption); ok {
		body, errVal := d.downcast(optionType.Type, bytes, to.Type)
		if errVal != nil {
			return super.Value{}, errVal
		}
		return super.NewValue(to, body.Bytes()), nil
	}
	return super.Value{}, d.errMismatch(typ, bytes, to)
}

func (d *downcast) toEnum(typ super.Type, bytes scode.Bytes, to *super.TypeEnum) (super.Value, *super.Value) {
	enumType, ok := typ.(*super.TypeEnum)
	if !ok {
		return super.Value{}, d.errMismatch(typ, bytes, to)
	}
	symbol, err := enumType.Symbol(int(super.DecodeUint(bytes)))
	if err != nil {
		return super.Value{}, d.errMismatch(typ, bytes, to)
	}
	i := to.Lookup(symbol)
	if i < 0 {
		return super.Value{}, d.errMismatch(typ, bytes, to)
	}
	return super.NewValue(to, super.EncodeUint(uint64(i))), nil

}

// subTypeOf finds the tag in the union array types that this value should be
// downcast to.  If the child value is a fusion value, then the type must match
// the subtype of the fusion value.  Otherwise, the child wasn't fused, and by
// definition of a fusion type, one of the union types must exactly match the
// child type.
func (d *downcast) subTypeOf(typ super.Type, bytes scode.Bytes, types []super.Type) (int, super.Type, []byte) {
	val := d.defuser.eval(super.NewValue(typ, bytes))
	typ, bytes = val.Type(), val.Bytes()
	return DowncastSubtypeIndex(types, typ), typ, bytes
}

// DowncastSubtypeIndex returns the index of typ in types.  If typ does not
// appear in types and typ is a record, DowncastSubtypeIndex returns the index
// in types of the first record type having all of typ's required fields.  If
// types contains no such record type, DowncastSubtypeIndex returns -1.
func DowncastSubtypeIndex(types []super.Type, typ super.Type) int {
	if i := slices.Index(types, typ); i >= 0 {
		return i
	}
	if fromRec, ok := typ.(*super.TypeRecord); ok {
		// Look for a record that has all fromRec's required fields.
		return slices.IndexFunc(types, func(t super.Type) bool {
			toRec, ok := t.(*super.TypeRecord)
			return ok && hasRequiredFields(fromRec, toRec)
		})
	}
	return -1
}

// hasRequiredFields returns true if every required field in from appears in to
// with the same type.
func hasRequiredFields(from, to *super.TypeRecord) bool {
	for _, f := range from.Fields {
		if !super.IsOptionType(f.Type) {
			typ, ok := to.TypeOfField(f.Name)
			if !ok || typ != f.Type {
				// Required field is absent or has wrong type.
				return false
			}
		}
	}
	return true
}

func (d *downcast) toError(typ super.Type, bytes scode.Bytes, to *super.TypeError) (super.Value, *super.Value) {
	if errorType, ok := typ.(*super.TypeError); ok {
		body, errVal := d.downcast(errorType.Type, bytes, to.Type)
		if errVal != nil {
			return super.Value{}, errVal
		}
		return super.NewValue(to, body.Bytes()), nil
	}
	return super.Value{}, d.errMismatch(typ, bytes, to)
}

func (d *downcast) errMissing(to super.Type) *super.Value {
	return d.sctx.NewErrorf("downcast: missing value in non-option type: %s", sup.FormatType(to)).Ptr()
}

func (d *downcast) errMismatch(typ super.Type, bytes []byte, to super.Type) *super.Value {
	return d.sctx.WrapError("downcast: type mismatch to "+sup.FormatType(to), super.NewValue(typ, bytes)).Ptr()
}

func (d *downcast) errSubtype(typ super.Type, bytes []byte, to super.Type) *super.Value {
	return d.sctx.WrapError("downcast: invalid subtype "+sup.FormatType(to), super.NewValue(typ, bytes)).Ptr()
}
