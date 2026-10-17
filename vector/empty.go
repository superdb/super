package vector

import (
	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sup"
)

// An Empty vector represents a vector with a type of zero length.
// These are needed to support recursive types since the invariant on union
// values is that the Type method may be called on a zero-length element of
// a union but zero-length values of recursel XXX
type Empty struct {
	typ super.Type
}

var _ Any = (*Empty)(nil)

func NewEmpty(typ super.Type) *Empty {
	return &Empty{typ}
}

func (e *Empty) Kind() Kind {
	typ := e.typ
	for {
		switch t := typ.(type) {
		case *super.TypeOfUint8, *super.TypeOfUint16, *super.TypeOfUint32, *super.TypeOfUint64:
			return KindUint
		case *super.TypeOfInt8, *super.TypeOfInt16, *super.TypeOfInt32, *super.TypeOfInt64, *super.TypeOfDuration, *super.TypeOfTime:
			return KindInt
		case *super.TypeOfFloat16, *super.TypeOfFloat32, *super.TypeOfFloat64:
			return KindFloat
		case *super.TypeOfBool:
			return KindBool
		case *super.TypeOfBytes:
			return KindBytes
		case *super.TypeOfString:
			return KindString
		case *super.TypeOfIP:
			return KindIP
		case *super.TypeOfNet:
			return KindNet
		case *super.TypeOfType:
			return KindType
		case *super.TypeOfNull:
			return KindNull
		case *super.TypeOfMissing:
			return KindMissing
		case *super.TypeRecord:
			return KindRecord
		case *super.TypeArray:
			return KindArray
		case *super.TypeSet:
			return KindSet
		case *super.TypeMap:
			return KindMap
		case *super.TypeUnion:
			return KindUnion
		case *super.TypeEnum:
			return KindEnum
		case *super.TypeError:
			return KindError
		case *super.TypeFusion:
			return KindFusion
		case *super.TypeOption:
			return KindOption
		case *super.TypeNamed:
			typ = t.Type
		default:
			panic(sup.String(e.typ))
		}
	}
}

func (e *Empty) Type() super.Type {
	return e.typ
}

func (*Empty) Len() uint32 {
	return 0
}

func (*Empty) Serialize(*scode.Builder, uint32) {}
