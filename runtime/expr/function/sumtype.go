package function

import (
	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

// SumType returns the union of named types formed from a map of type names
// to types, i.e., sum_type(map{"A":<T1>,"B":<T2>}) is <A=T1|B=T2>.
type SumType struct {
	sctx *super.Context
}

func (s *SumType) Call(args ...vector.Any) vector.Any {
	vec := args[0]
	switch vec.Kind() {
	case vector.KindError, vector.KindNull:
		return vec
	}
	typ, ok := super.TypeUnder(vec.Type()).(*super.TypeMap)
	if !ok || super.TypeUnder(typ.KeyType) != super.TypeString || super.TypeUnder(typ.ValType) != super.TypeType {
		return vector.NewWrappedError(s.sctx, "sum_type: argument is not a map of strings to types", vec)
	}
	if c, ok := vec.(*vector.Const); ok {
		return vector.NewConst(s.call(c.Any, 0), c.Len())
	}
	b := vector.NewDynamicValueBuilder()
	for i := range vec.Len() {
		out := s.call(vec, i)
		b.Write(vector.ValueAt(nil, out, 0))
	}
	return b.Build(s.sctx)
}

// call returns a vector of length one with the sum type for the map at slot
// of vec or an error.
func (s *SumType) call(vec vector.Any, slot uint32) vector.Any {
	var b scode.Builder
	val := vector.ValueAt(&b, vec, slot)
	var types []super.Type
	for it := val.ContainerIter(); !it.Done(); {
		name := super.DecodeString(it.Next())
		typ, err := s.sctx.LookupByValue(it.Next())
		if err != nil {
			panic(err)
		}
		named, err := s.sctx.LookupTypeNamed(name, typ)
		if err != nil {
			return vector.NewWrappedError(s.sctx, "sum_type: "+err.Error(), vector.Pick(vec, []uint32{slot}))
		}
		types = append(types, named)
	}
	var out super.Type
	switch len(types) {
	case 0:
		return vector.NewWrappedError(s.sctx, "sum_type: empty map", vector.Pick(vec, []uint32{slot}))
	case 1:
		// A union of one type is just that type.
		out = types[0]
	default:
		out = s.sctx.MustLookupTypeUnion(types)
	}
	return vector.NewTypeValue([]super.Type{out})
}
