package vbuild

import (
	"net/netip"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type Builder interface {
	Write(vector.Any)
	Build() vector.Any
}

func New(typ super.Type) Builder {
	switch typ := typ.(type) {
	case *super.TypeOfUint8,
		*super.TypeOfUint16,
		*super.TypeOfUint32,
		*super.TypeOfUint64:
		return &genericBuilder[uint64]{
			valuesOf: func(vec vector.Any) []uint64 { return vec.(*vector.Uint).Values },
			build: func(vals []uint64) vector.Any {
				return vector.NewUint(typ, vals)
			},
		}
	case *super.TypeOfInt8,
		*super.TypeOfInt16,
		*super.TypeOfInt32,
		*super.TypeOfInt64,
		*super.TypeOfDuration,
		*super.TypeOfTime:
		return &genericBuilder[int64]{
			valuesOf: func(vec vector.Any) []int64 { return vec.(*vector.Int).Values },
			build: func(vals []int64) vector.Any {
				return vector.NewInt(typ, vals)
			},
		}
	case *super.TypeOfFloat16,
		*super.TypeOfFloat32,
		*super.TypeOfFloat64:
		return &genericBuilder[float64]{
			valuesOf: func(vec vector.Any) []float64 { return vec.(*vector.Float).Values },
			build: func(vals []float64) vector.Any {
				return vector.NewFloat(typ, vals)
			},
		}
	case *super.TypeOfBool:
		return &boolBuilder{}
	case *super.TypeOfString,
		*super.TypeOfBytes:
		return newBytesBuilder(typ)
	case *super.TypeOfIP:
		return &genericBuilder[netip.Addr]{
			valuesOf: func(vec vector.Any) []netip.Addr { return vec.(*vector.IP).Values },
			build: func(vals []netip.Addr) vector.Any {
				return vector.NewIP(vals)
			},
		}
	case *super.TypeOfNet:
		return &genericBuilder[netip.Prefix]{
			valuesOf: func(vec vector.Any) []netip.Prefix { return vec.(*vector.Net).Values },
			build: func(vals []netip.Prefix) vector.Any {
				return vector.NewNet(vals)
			},
		}
	case *super.TypeOfType:
		return &genericBuilder[super.Type]{
			valuesOf: func(vec vector.Any) []super.Type { return vec.(*vector.TypeValue).Types() },
			build: func(vals []super.Type) vector.Any {
				return vector.NewTypeValue(vals)
			},
		}
	case *super.TypeOfNull:
		return &nullBuilder{}
	case *super.TypeOfMissing:
		return &missingBuilder{}
	case *super.TypeRecord:
		return newRecordBuilder(typ)
	case *super.TypeArray, *super.TypeSet:
		return newArraySetBuilder(typ)
	case *super.TypeMap:
		return newMapBuilder(typ)
	case *super.TypeUnion:
		return newUnionBuilder(typ)
	case *super.TypeEnum:
		return newEnumBuilder(typ)
	case *super.TypeError:
		return &errorBuilder{typ: typ, vals: New(typ.Type)}
	case *super.TypeNamed:
		return newNamedBuilder(typ)
	case *super.TypeFusion:
		if super.IsTypeAny(typ) {
			return newAnyBuilder(typ)
		}
		return newFusionBuilder(typ)
	case *super.TypeOption:
		return newOptionBuilder(typ)
	default:
		panic(typ)
	}
}
