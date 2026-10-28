package super

import (
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrNotArray  = errors.New("cannot index a non-array")
	ErrIndex     = errors.New("array index out of bounds")
	ErrUnionTag  = errors.New("invalid union tag")
	ErrEnumIndex = errors.New("enum index out of bounds")
)

// A Type is an interface presented by a zeek type.
// Types can be used to infer type compatibility and create new values
// of the underlying type.
type Type interface {
	// ID returns a unique (per Context) identifier that
	// represents this type.  For a named type, this identifier
	// represents the underlying type and not the named type itself.
	// Callers that care about the underlying type of a Value for
	// example should prefer to use this instead of using a Go
	// type assertion on a Type instance.
	ID() int
	Kind() Kind
}

type Kind int

const (
	PrimitiveKind Kind = iota
	RecordKind
	ArrayKind
	SetKind
	MapKind
	UnionKind
	EnumKind
	ErrorKind
	FusionKind
	OptionKind
	DecimalKind
)

func (k Kind) String() string {
	switch k {
	case PrimitiveKind:
		return "primitive"
	case RecordKind:
		return "record"
	case ArrayKind:
		return "array"
	case SetKind:
		return "set"
	case MapKind:
		return "map"
	case UnionKind:
		return "union"
	case EnumKind:
		return "enum"
	case DecimalKind:
		return "decimal"
	case OptionKind:
		return "option"
	case ErrorKind:
		return "error"
	default:
		return fmt.Sprintf("<unknown kind: %d>", k)
	}
}

var (
	TypeUint8     = &TypeOfUint8{}
	TypeUint16    = &TypeOfUint16{}
	TypeUint32    = &TypeOfUint32{}
	TypeUint64    = &TypeOfUint64{}
	TypeInt8      = &TypeOfInt8{}
	TypeInt16     = &TypeOfInt16{}
	TypeInt32     = &TypeOfInt32{}
	TypeInt64     = &TypeOfInt64{}
	TypeDuration  = &TypeOfDuration{}
	TypeTimestamp = &TypeOfTimestamp{}
	TypeFloat16   = &TypeOfFloat16{}
	TypeFloat32   = &TypeOfFloat32{}
	TypeFloat64   = &TypeOfFloat64{}
	TypeBool      = &TypeOfBool{}
	TypeBytes     = &TypeOfBytes{}
	TypeString    = &TypeOfString{}
	TypeIP        = &TypeOfIP{}
	TypeNet       = &TypeOfNet{}
	TypeUUID      = &TypeOfUUID{}
	TypeType      = &TypeOfType{}
	TypeNull      = &TypeOfNull{}
	TypeMissing   = &TypeOfMissing{}
	TypeNone      = &TypeOfNone{}
	TypeAll       = &TypeOfAll{}
	// TypeAny is the top type of the type system, to which every type
	// conforms.  It exists only in the type system: no value has type
	// any.  A value cast to any (or stored where any is expected) has type
	// fusion(all), the data model's type for a value of any type, which
	// carries the value's own type (see Context.AnyToFusion).
	TypeAny = &TypeOfAny{}
)

// Primitive types whose IDs are reserved but whose values are not yet
// implemented.  See TypeOfUnimplemented.
var (
	TypeUint128       = &TypeOfUnimplemented{IDUint128, "uint128"}
	TypeInt128        = &TypeOfUnimplemented{IDInt128, "int128"}
	TypeTimestamp0    = &TypeOfUnimplemented{IDTimestamp0, "timestamp(0)"}
	TypeTimestamp3    = &TypeOfUnimplemented{IDTimestamp3, "timestamp(3)"}
	TypeTimestamp6    = &TypeOfUnimplemented{IDTimestamp6, "timestamp(6)"}
	TypeTimestampNTZ0 = &TypeOfUnimplemented{IDTimestampNTZ0, "timestamp_ntz(0)"}
	TypeTimestampNTZ3 = &TypeOfUnimplemented{IDTimestampNTZ3, "timestamp_ntz(3)"}
	TypeTimestampNTZ6 = &TypeOfUnimplemented{IDTimestampNTZ6, "timestamp_ntz(6)"}
	TypeTimestampNTZ  = &TypeOfUnimplemented{IDTimestampNTZ, "timestamp_ntz"}
	TypeDate          = &TypeOfUnimplemented{IDDate, "date"}
	TypeTime0         = &TypeOfUnimplemented{IDTime0, "time(0)"}
	TypeTime3         = &TypeOfUnimplemented{IDTime3, "time(3)"}
	TypeTime6         = &TypeOfUnimplemented{IDTime6, "time(6)"}
	TypeTime          = &TypeOfUnimplemented{IDTime, "time"}
	TypeInterval      = &TypeOfUnimplemented{IDInterval, "interval"}
	TypeGeometry      = &TypeOfUnimplemented{IDGeometry, "geometry"}
	TypeGeography     = &TypeOfUnimplemented{IDGeography, "geography"}
)

var unimplementedTypes = []*TypeOfUnimplemented{
	TypeUint128,
	TypeInt128,
	TypeTimestamp0,
	TypeTimestamp3,
	TypeTimestamp6,
	TypeTimestampNTZ0,
	TypeTimestampNTZ3,
	TypeTimestampNTZ6,
	TypeTimestampNTZ,
	TypeDate,
	TypeTime0,
	TypeTime3,
	TypeTime6,
	TypeTime,
	TypeInterval,
	TypeGeometry,
	TypeGeography,
}

// Primary Type IDs
//
// The IDs are grouped so that the IsInteger, IsSigned, IsUnsigned, IsFloat,
// and IsNumber predicates can be computed from ID ranges.  The timestamp,
// timestamp_ntz, and time types are parameterized by a fractional-second
// precision p (0, 3, 6, or 9 digits) with each precision having its own ID.
// The unadorned names (timestamp, timestamp_ntz, time) have precision 9.
// The geometry and geography types are planar and spherical shapes encoded
// as OGC well-known binary (WKB) in the default coordinate reference system
// (OGC:CRS84).  Other reference systems will be parameterized types.
// IDs 41 through 47 are reserved for future primitives.

const (
	IDUint8         = 0
	IDUint16        = 1
	IDUint32        = 2
	IDUint64        = 3
	IDUint128       = 4
	IDInt8          = 5
	IDInt16         = 6
	IDInt32         = 7
	IDInt64         = 8
	IDInt128        = 9
	IDDuration      = 10
	IDTimestamp0    = 11
	IDTimestamp3    = 12
	IDTimestamp6    = 13
	IDTimestamp     = 14 // timestamp(9)
	IDTimestampNTZ0 = 15
	IDTimestampNTZ3 = 16
	IDTimestampNTZ6 = 17
	IDTimestampNTZ  = 18 // timestamp_ntz(9)
	IDDate          = 19
	IDTime0         = 20
	IDTime3         = 21
	IDTime6         = 22
	IDTime          = 23 // time(9)
	IDFloat16       = 24
	IDFloat32       = 25
	IDFloat64       = 26
	IDBool          = 27
	IDBytes         = 28
	IDString        = 29
	IDIP            = 30
	IDNet           = 31
	IDInterval      = 32
	IDUUID          = 33
	IDGeometry      = 34
	IDGeography     = 35
	IDType          = 36
	IDNull          = 37
	IDMissing       = 38
	IDNone          = 39
	IDAll           = 40
	IDAny           = 41
	IDTypeComplex   = 48
)

// Encodings for complex type values.

const (
	TypeValueRecord  = 48
	TypeValueArray   = 49
	TypeValueSet     = 50
	TypeValueMap     = 51
	TypeValueUnion   = 52
	TypeValueEnum    = 53
	TypeValueError   = 54
	TypeValueNameDef = 55
	TypeValueNameRef = 56
	TypeValueFusion  = 57
	TypeValueOption  = 58
	TypeValueDecimal = 59
	TypeValueMax     = TypeValueDecimal
)

// True iff the type id is a signed or unsigened integer.
func IsInteger(id int) bool {
	return id <= IDInt128
}

// True iff the type id is a signed or unsigned integer, duration, a
// timestamp, date, or time type, or a float.
func IsNumber(id int) bool {
	return id <= IDFloat64
}

// True iff the type id is encoded as a float encoding.
func IsFloat(id int) bool {
	return id >= IDFloat16 && id <= IDFloat64
}

// True iff the type id is encoded as a number encoding and is signed.
func IsSigned(id int) bool {
	return id >= IDInt8 && id <= IDTime
}

// True iff the type id is encoded as a number encoding and is unsigned.
func IsUnsigned(id int) bool {
	return id <= IDUint128
}

func LookupPrimitive(name string) Type {
	switch name {
	case "uint8":
		return TypeUint8
	case "uint16":
		return TypeUint16
	case "uint32":
		return TypeUint32
	case "uint64":
		return TypeUint64
	case "int8":
		return TypeInt8
	case "int16":
		return TypeInt16
	case "int32":
		return TypeInt32
	case "int64":
		return TypeInt64
	case "duration":
		return TypeDuration
	case "timestamp", "timestamp(9)":
		return TypeTimestamp
	case "float16":
		return TypeFloat16
	case "float32":
		return TypeFloat32
	case "float64":
		return TypeFloat64
	case "bool":
		return TypeBool
	case "bytes":
		return TypeBytes
	case "string":
		return TypeString
	case "ip":
		return TypeIP
	case "net":
		return TypeNet
	case "type":
		return TypeType
	case "null":
		return TypeNull
	case "missing":
		return TypeMissing
	case "none":
		return TypeNone
	case "all":
		return TypeAll
	case "any":
		return TypeAny
	case "uuid":
		return TypeUUID
	case "timestamp_ntz(9)":
		return TypeTimestampNTZ
	case "time(9)":
		return TypeTime
	}
	for _, typ := range unimplementedTypes {
		if typ.name == name {
			return typ
		}
	}
	return nil
}

func PrimitiveName(typ Type) string {
	switch typ.(type) {
	case *TypeOfUint8:
		return "uint8"
	case *TypeOfUint16:
		return "uint16"
	case *TypeOfUint32:
		return "uint32"
	case *TypeOfUint64:
		return "uint64"
	case *TypeOfInt8:
		return "int8"
	case *TypeOfInt16:
		return "int16"
	case *TypeOfInt32:
		return "int32"
	case *TypeOfInt64:
		return "int64"
	case *TypeOfDuration:
		return "duration"
	case *TypeOfTimestamp:
		return "timestamp"
	case *TypeOfFloat16:
		return "float16"
	case *TypeOfFloat32:
		return "float32"
	case *TypeOfFloat64:
		return "float64"
	case *TypeOfBool:
		return "bool"
	case *TypeOfBytes:
		return "bytes"
	case *TypeOfString:
		return "string"
	case *TypeOfIP:
		return "ip"
	case *TypeOfNet:
		return "net"
	case *TypeOfType:
		return "type"
	case *TypeOfNull:
		return "null"
	case *TypeOfMissing:
		return "missing"
	case *TypeOfNone:
		return "none"
	case *TypeOfAll:
		return "all"
	case *TypeOfAny:
		return "any"
	case *TypeOfUUID:
		return "uuid"
	case *TypeOfUnimplemented:
		return typ.(*TypeOfUnimplemented).name
	default:
		return fmt.Sprintf("unknown primitive type: %T", typ)
	}
}

func LookupPrimitiveByID(id int) (Type, error) {
	if id < 0 {
		return nil, fmt.Errorf("negative type ID: %d", id)
	}
	if id >= IDTypeComplex {
		return nil, fmt.Errorf("type ID too large for primitive: %d", id)
	}
	switch id {
	case IDBool:
		return TypeBool, nil
	case IDInt8:
		return TypeInt8, nil
	case IDUint8:
		return TypeUint8, nil
	case IDInt16:
		return TypeInt16, nil
	case IDUint16:
		return TypeUint16, nil
	case IDInt32:
		return TypeInt32, nil
	case IDUint32:
		return TypeUint32, nil
	case IDInt64:
		return TypeInt64, nil
	case IDUint64:
		return TypeUint64, nil
	case IDFloat16:
		return TypeFloat16, nil
	case IDFloat32:
		return TypeFloat32, nil
	case IDFloat64:
		return TypeFloat64, nil
	case IDBytes:
		return TypeBytes, nil
	case IDString:
		return TypeString, nil
	case IDIP:
		return TypeIP, nil
	case IDNet:
		return TypeNet, nil
	case IDTimestamp:
		return TypeTimestamp, nil
	case IDDuration:
		return TypeDuration, nil
	case IDType:
		return TypeType, nil
	case IDNull:
		return TypeNull, nil
	case IDMissing:
		return TypeMissing, nil
	case IDNone:
		return TypeNone, nil
	case IDAll:
		return TypeAll, nil
	case IDAny:
		return TypeAny, nil
	case IDUUID:
		return TypeUUID, nil
	}
	for _, typ := range unimplementedTypes {
		if typ.id == id {
			return typ, nil
		}
	}
	return nil, fmt.Errorf("primitive type ID %d not implemented", id)
}

// Utilities shared by complex types (ie, set and array)

// InnerType returns the element type for the underlying set or array type or
// nil if the underlying type is not a set or array.
func InnerType(typ Type) Type {
	switch typ := TypeUnder(typ).(type) {
	case *TypeSet:
		return typ.Type
	case *TypeArray:
		return typ.Type
	default:
		return nil
	}
}

func IsUnionType(typ Type) bool {
	_, ok := TypeUnder(typ).(*TypeUnion)
	return ok
}

func IsRecordType(typ Type) bool {
	_, ok := TypeUnder(typ).(*TypeRecord)
	return ok
}

func IsTypeNamed(typ Type) bool {
	_, ok := typ.(*TypeNamed)
	return ok
}

func TypeRecordOf(typ Type) *TypeRecord {
	t, _ := TypeUnder(typ).(*TypeRecord)
	return t
}

func IsContainerType(typ Type) bool {
	switch typ := typ.(type) {
	case *TypeNamed:
		return IsContainerType(typ.Type)
	case *TypeSet, *TypeArray, *TypeRecord, *TypeUnion, *TypeMap:
		return true
	default:
		return false
	}
}

func IsPrimitiveType(typ Type) bool {
	return !IsContainerType(typ)
}

func TypeID(typ Type) int {
	if named, ok := typ.(*TypeNamed); ok {
		return named.id
	}
	return typ.ID()
}

// UniqueTypes returns the set of unique Types in types in sorted
// order. types will be sorted and deduplicated in place.
func UniqueTypes(types []Type) []Type {
	sort.SliceStable(types, func(i, j int) bool {
		return CompareTypes(types[i], types[j]) < 0
	})
	out := types[:0]
	var prev Type
	for _, typ := range types {
		if typ != prev {
			out = append(out, typ)
			prev = typ
		}
	}
	return out
}

func CompareTypes(a, b Type) int {
	aID, bID := TypeID(a), TypeID(b)
	if aID == bID {
		return 0
	}
	// Named types come first.  We are careful here to not look into the
	// Type field of the named type as we need to be able to order types
	// (e.g., to create union types) while a named type is not fully resolved.
	if a, ok := a.(*TypeNamed); ok {
		if b, ok := b.(*TypeNamed); ok {
			if a.Name < b.Name {
				return -1
			}
			if a.Name > b.Name {
				return 1
			}
		}
		return -1
	}
	if IsTypeNamed(b) {
		return 1
	}
	if cmp := cmp.Compare(a.Kind(), b.Kind()); cmp != 0 {
		return cmp
	}
	a, b = TypeUnder(a), TypeUnder(b)
	switch a.Kind() {
	case PrimitiveKind:
		return cmp.Compare(aID, bID)
	case RecordKind:
		ra, rb := TypeRecordOf(a), TypeRecordOf(b)
		// First compare number of fields.
		if cmp := cmp.Compare(len(ra.Fields), len(rb.Fields)); cmp != 0 {
			return cmp
		}
		// Second compare field names.
		for i := range ra.Fields {
			if cmp := strings.Compare(ra.Fields[i].Name, rb.Fields[i].Name); cmp != 0 {
				return cmp
			}
		}
		// Lastly compare field types.
		for i := range ra.Fields {
			if cmp := CompareTypes(ra.Fields[i].Type, rb.Fields[i].Type); cmp != 0 {
				return cmp
			}
		}
		return 0
	case ArrayKind, SetKind:
		a, b = InnerType(a), InnerType(b)
		return CompareTypes(a, b)
	case MapKind:
		ma, mb := a.(*TypeMap), b.(*TypeMap)
		if cmp := CompareTypes(ma.KeyType, mb.KeyType); cmp != 0 {
			return cmp
		}
		return CompareTypes(ma.ValType, mb.ValType)
	case UnionKind:
		ua, ub := a.(*TypeUnion), b.(*TypeUnion)
		if cmp := cmp.Compare(len(ua.Types), len(ub.Types)); cmp != 0 {
			return cmp
		}
		for i := range ua.Types {
			if cmp := CompareTypes(ua.Types[i], ub.Types[i]); cmp != 0 {
				return cmp
			}
		}
		return 0
	case EnumKind:
		ea, eb := a.(*TypeEnum), b.(*TypeEnum)
		if cmp := cmp.Compare(len(ea.Symbols), len(eb.Symbols)); cmp != 0 {
			return cmp
		}
		for i := range ea.Symbols {
			if cmp := strings.Compare(ea.Symbols[i], eb.Symbols[i]); cmp != 0 {
				return cmp
			}
		}
		return 0
	case ErrorKind:
		ea, eb := a.(*TypeError), b.(*TypeError)
		return CompareTypes(ea.Type, eb.Type)
	case DecimalKind:
		da, db := a.(*TypeDecimal), b.(*TypeDecimal)
		if cmp := cmp.Compare(da.Precision, db.Precision); cmp != 0 {
			return cmp
		}
		return cmp.Compare(da.Scale, db.Scale)
	}
	return 0
}

type TypeOfType struct{}

func (t *TypeOfType) ID() int {
	return IDType
}

func (t *TypeOfType) Kind() Kind {
	return PrimitiveKind
}

func EncodeTypeValue(t Type) []byte {
	return AppendTypeValue(nil, t)
}

func AppendTypeValue(b []byte, t Type) []byte {
	var typedefs map[string]Type
	return appendTypeValue(b, t, &typedefs)
}

func appendTypeValue(b []byte, t Type, typedefs *map[string]Type) []byte {
	switch t := t.(type) {
	case *TypeNamed:
		if *typedefs == nil {
			*typedefs = make(map[string]Type)
		}
		id := byte(TypeValueNameDef)
		if previous, ok := (*typedefs)[t.Name]; ok {
			if previous != t.Type {
				panic(t.Type)
			}
			id = TypeValueNameRef
		} else {
			(*typedefs)[t.Name] = t.Type
		}
		b = append(b, id)
		b = binary.AppendUvarint(b, uint64(len(t.Name)))
		b = append(b, t.Name...)
		if id == TypeValueNameRef {
			return b
		}
		return appendTypeValue(b, t.Type, typedefs)
	case *TypeRecord:
		b = append(b, TypeValueRecord)
		n := len(t.Fields)
		b = binary.AppendUvarint(b, uint64(n))
		for _, f := range t.Fields {
			b = binary.AppendUvarint(b, uint64(len(f.Name)))
			b = append(b, f.Name...)
			b = appendTypeValue(b, f.Type, typedefs)
		}
		return b
	case *TypeUnion:
		b = append(b, TypeValueUnion)
		b = binary.AppendUvarint(b, uint64(len(t.Types)))
		for _, t := range t.Types {
			b = appendTypeValue(b, t, typedefs)
		}
		return b
	case *TypeSet:
		b = append(b, TypeValueSet)
		return appendTypeValue(b, t.Type, typedefs)
	case *TypeArray:
		b = append(b, TypeValueArray)
		return appendTypeValue(b, t.Type, typedefs)
	case *TypeDecimal:
		b = append(b, TypeValueDecimal)
		b = binary.AppendUvarint(b, uint64(t.Precision))
		return binary.AppendUvarint(b, uint64(t.Scale))
	case *TypeEnum:
		b = append(b, TypeValueEnum)
		b = binary.AppendUvarint(b, uint64(len(t.Symbols)))
		for _, s := range t.Symbols {
			b = binary.AppendUvarint(b, uint64(len(s)))
			b = append(b, s...)
		}
		return b
	case *TypeMap:
		b = append(b, TypeValueMap)
		b = appendTypeValue(b, t.KeyType, typedefs)
		return appendTypeValue(b, t.ValType, typedefs)
	case *TypeError:
		b = append(b, TypeValueError)
		return appendTypeValue(b, t.Type, typedefs)
	case *TypeFusion:
		b = append(b, TypeValueFusion)
		return appendTypeValue(b, t.Type, typedefs)
	case *TypeOption:
		b = append(b, TypeValueOption)
		return appendTypeValue(b, t.Type, typedefs)
	default:
		// Primitive type
		return append(b, byte(t.ID()))
	}
}
