package coerce

import (
	"cmp"
	"fmt"
	"math"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/sup"
)

// ComparisonType returns the type to which values of types a and b are
// coerced by a comparison operator, or false if such values can't be
// compared.  The type checker and the runtime both use it so they agree on
// which comparisons are errors.
//
// Values of the same type are comparable, integers and floats are promoted
// as for arithmetic, and records (with the same field names), arrays, sets,
// and maps are comparable when their elements are.  A nested union is
// comparable with a type that one of its types holds or with a union that
// shares one of its types.  (The caller handles a top-level union by
// comparing each of its types.)  Names are ignored.  A value of unknown type
// (any or fusion(all)) is comparable with anything.
//
// A fusion type, which only the type checker has, stands for values that
// each have one of the fused types, so every one of those types must be
// comparable with the other type.  hasFusion reports whether a type is or
// contains a fusion type (see function.FusionChecker).
func ComparisonType(sctx *super.Context, hasFusion func(super.Type) bool, a, b super.Type) (super.Type, bool) {
	if a == b && !hasFusion(a) {
		return a, true
	}
	if IsUnknown(a) || IsUnknown(b) {
		return super.TypeAny, true
	}
	aFused, bFused := isFusion(a), isFusion(b)
	a, b = Under(a), Under(b)
	if IsUnknown(a) || IsUnknown(b) {
		return super.TypeAny, true
	}
	if aFused || bFused {
		for _, ta := range fusedTypes(a, aFused) {
			for _, tb := range fusedTypes(b, bFused) {
				if _, ok := ComparisonType(sctx, hasFusion, ta, tb); !ok {
					return nil, false
				}
			}
		}
		return super.TypeAny, true
	}
	if a == b && !hasFusion(a) {
		return a, true
	}
	// Type none has no values (e.g., it's the element type of an empty
	// array), so it compares with any type.
	if a == super.TypeNone {
		return b, true
	}
	if b == super.TypeNone {
		return a, true
	}
	a, aNull := withoutNull(sctx, a)
	b, bNull := withoutNull(sctx, b)
	if aNull || bNull {
		if a == nil {
			a = b
		} else if b == nil {
			b = a
		}
		if a == nil {
			return super.TypeNull, true
		}
		typ, ok := ComparisonType(sctx, hasFusion, a, b)
		if !ok {
			return nil, false
		}
		if IsUnknown(typ) {
			return typ, true
		}
		return sctx.Nullable(typ), true
	}
	if aid, bid := a.ID(), b.ID(); isNumeric(aid) && isNumeric(bid) {
		id, err := Promote(super.NewValue(a, nil), super.NewValue(b, nil))
		if err != nil {
			return nil, false
		}
		typ, err := super.LookupPrimitiveByID(id)
		return typ, err == nil
	}
	_, aUnion := a.(*super.TypeUnion)
	_, bUnion := b.(*super.TypeUnion)
	if aUnion || bUnion {
		return unionComparisonType(sctx, hasFusion, a, b)
	}
	switch a := a.(type) {
	case *super.TypeRecord:
		b, ok := b.(*super.TypeRecord)
		if !ok || len(a.Fields) != len(b.Fields) {
			return nil, false
		}
		var unknown bool
		fields := make([]super.Field, len(a.Fields))
		for i, f := range a.Fields {
			if f.Name != b.Fields[i].Name {
				return nil, false
			}
			typ, ok := ComparisonType(sctx, hasFusion, f.Type, b.Fields[i].Type)
			if !ok {
				return nil, false
			}
			unknown = unknown || IsUnknown(typ)
			fields[i] = super.NewField(f.Name, typ)
		}
		if unknown {
			return super.TypeAny, true
		}
		typ, err := sctx.LookupTypeRecord(fields)
		return typ, err == nil
	case *super.TypeArray:
		if b, ok := b.(*super.TypeArray); ok {
			typ, ok := ComparisonType(sctx, hasFusion, a.Type, b.Type)
			if !ok || IsUnknown(typ) {
				return typ, ok
			}
			return sctx.LookupTypeArray(typ), true
		}
	case *super.TypeSet:
		if b, ok := b.(*super.TypeSet); ok {
			typ, ok := ComparisonType(sctx, hasFusion, a.Type, b.Type)
			if !ok || IsUnknown(typ) {
				return typ, ok
			}
			return sctx.LookupTypeSet(typ), true
		}
	case *super.TypeMap:
		if b, ok := b.(*super.TypeMap); ok {
			key, ok := ComparisonType(sctx, hasFusion, a.KeyType, b.KeyType)
			if !ok {
				return nil, false
			}
			val, ok := ComparisonType(sctx, hasFusion, a.ValType, b.ValType)
			if !ok {
				return nil, false
			}
			if IsUnknown(key) || IsUnknown(val) {
				return super.TypeAny, true
			}
			return sctx.LookupTypeMap(key, val), true
		}
	case *super.TypeOption:
		if b, ok := b.(*super.TypeOption); ok {
			typ, ok := ComparisonType(sctx, hasFusion, a.Type, b.Type)
			if !ok || IsUnknown(typ) {
				return typ, ok
			}
			return sctx.LookupTypeOption(typ), true
		}
	}
	return nil, false
}

// unionComparisonType returns the union of the types of a and b, at least
// one of which is a union, if they share a type.  A type of one that's
// comparable with a type of the other only by promoting it isn't shared
// since a value keeps its type inside a union.
func unionComparisonType(sctx *super.Context, hasFusion func(super.Type) bool, a, b super.Type) (super.Type, bool) {
	aTypes := unionTypes(a)
	types := slices.Clone(aTypes)
	var shared bool
next:
	for _, tb := range unionTypes(b) {
		for _, ta := range aTypes {
			typ, ok := ComparisonType(sctx, hasFusion, ta, tb)
			if !ok {
				continue
			}
			if IsUnknown(typ) {
				return super.TypeAny, true
			}
			if typ == ta && typ == tb {
				shared = true
				continue next
			}
		}
		types = append(types, tb)
	}
	if !shared {
		return nil, false
	}
	return sctx.LookupTypeUnion(types)
}

func unionTypes(typ super.Type) []super.Type {
	if union, ok := typ.(*super.TypeUnion); ok {
		return union.Types
	}
	return []super.Type{typ}
}

// fusedTypes returns the types that a value of type typ, which was fused if
// fused is true, may have.
func fusedTypes(typ super.Type, fused bool) []super.Type {
	if fused {
		return unionTypes(typ)
	}
	return []super.Type{typ}
}

func isFusion(typ super.Type) bool {
	_, ok := super.TypeUnder(typ).(*super.TypeFusion)
	return ok
}

// Containable reports whether a value of type a can be compared with a value
// of type b or with some value nested in it, as the in operator requires.
// The type checker and the runtime both use it so they agree on which uses
// of in are errors.  A null in a union doesn't count since that would make
// anything containable in a nullable type.  (The caller handles a top-level
// union by checking each of its types.)
func Containable(sctx *super.Context, hasFusion func(super.Type) bool, a, b super.Type) bool {
	if _, ok := ComparisonType(sctx, hasFusion, a, b); ok {
		return true
	}
	switch b := Under(b).(type) {
	case *super.TypeOfNone:
		return true
	case *super.TypeRecord:
		for _, f := range b.Fields {
			if Containable(sctx, hasFusion, a, f.Type) {
				return true
			}
		}
	case *super.TypeArray:
		return Containable(sctx, hasFusion, a, b.Type)
	case *super.TypeSet:
		return Containable(sctx, hasFusion, a, b.Type)
	case *super.TypeMap:
		return Containable(sctx, hasFusion, a, b.KeyType) || Containable(sctx, hasFusion, a, b.ValType)
	case *super.TypeUnion:
		for _, t := range b.Types {
			if t != super.TypeNull && Containable(sctx, hasFusion, a, t) {
				return true
			}
		}
	case *super.TypeOption:
		return Containable(sctx, hasFusion, a, b.Type)
	case *super.TypeError:
		return Containable(sctx, hasFusion, a, b.Type)
	}
	return false
}

// InMismatch returns the message of the error for the in operator when
// its left-hand side isn't containable in a right-hand side of type b.
func InMismatch(b super.Type) string {
	switch b := Under(b).(type) {
	case *super.TypeArray:
		return "left-hand side of in operator not compatible with array type"
	case *super.TypeSet:
		return "left-hand side of in operator not compatible with set type"
	case *super.TypeRecord:
		return "left-hand side of in operator not compatible with any fields of record on right-hand side"
	case *super.TypeMap:
		return "left-hand side of in operator not compatible with map value or key type"
	case *super.TypeOfNet:
		return "right-hand side of in operator cannot be type net; consider cidr_match()"
	default:
		return "scalar type mismatch for 'in' operator where right-hand side is not container type: " + sup.FormatType(b)
	}
}

// TypeMismatch returns the message of the error for a comparison with
// operator op of values of types a and b that can't be compared.
func TypeMismatch(op string, a, b super.Type) string {
	return fmt.Sprintf("type mismatch: %s %s %s", sup.FormatType(a), op, sup.FormatType(b))
}

// isNumeric reports whether id is the ID of an integer or float type.
// Unlike super.IsNumber, it excludes duration and time types, which are
// comparable only with themselves.
func isNumeric(id int) bool {
	return super.IsInteger(id) || super.IsFloat(id)
}

// Under returns typ without its names and (as for a type from the type
// checker) without its fusion types.
func Under(typ super.Type) super.Type {
	for {
		switch t := typ.(type) {
		case *super.TypeNamed:
			typ = t.Type
		case *super.TypeFusion:
			if t.Type == super.TypeAll {
				return super.TypeAny
			}
			typ = t.Type
		default:
			return typ
		}
	}
}

// IsUnknown reports whether typ is any or fusion(all), the type of a value
// whose type isn't known.
func IsUnknown(typ super.Type) bool {
	return typ == super.TypeAny || super.IsFusionAll(typ)
}

// withoutNull returns typ without null and whether typ is null or a union
// with null.  The returned type is nil if typ is null.
func withoutNull(sctx *super.Context, typ super.Type) (super.Type, bool) {
	if typ == super.TypeNull {
		return nil, true
	}
	union, tag := super.NullableUnion(typ)
	if union == nil {
		return typ, false
	}
	var types []super.Type
	for i, t := range union.Types {
		if i != tag {
			types = append(types, t)
		}
	}
	if len(types) == 1 {
		return types[0], true
	}
	return sctx.MustLookupTypeUnion(types), true
}

// CompareFloats is like cmp.Compare but orders NaN after all other values
// (as Postgres does) rather than before them.
func CompareFloats(a, b float64) int {
	aNaN, bNaN := math.IsNaN(a), math.IsNaN(b)
	if aNaN || bNaN {
		return cmp.Compare(btoi(aNaN), btoi(bNaN))
	}
	return cmp.Compare(a, b)
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
