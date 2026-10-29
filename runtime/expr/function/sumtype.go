package function

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
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
	if vec, ok := expr.Catch(s.sctx, expr.CatchAll, vec, "sum_type"); ok {
		return vec
	}
	typ, ok := super.TypeUnder(vec.Type()).(*super.TypeMap)
	if !ok || super.TypeUnder(typ.KeyType) != super.TypeString || super.TypeUnder(typ.ValType) != super.TypeType {
		return vector.NewWrappedError(s.sctx, "sum_type: argument is not a map of strings to types", vec)
	}
	var sb scode.Builder
	if c, ok := vec.(*vector.Const); ok {
		return vector.NewConstFromValue(s.sctx, s.call(&sb, c, 0), c.Len())
	}
	b := vector.NewDynamicValueBuilder()
	for i := range vec.Len() {
		b.Write(s.call(&sb, vec, i))
	}
	return b.Build(s.sctx)
}

// call returns the sum type for the map at slot of vec or an error.
func (s *SumType) call(sb *scode.Builder, vec vector.Any, slot uint32) super.Value {
	val := vector.ValueAt(sb, vec, slot)
	var types []super.Type
	for it := val.ContainerIter(); !it.Done(); {
		name := super.DecodeString(it.Next())
		typ, err := s.sctx.LookupByValue(it.Next())
		if err != nil {
			panic(err)
		}
		named, err := s.sctx.LookupTypeNamed(name, typ)
		if err != nil {
			return s.sctx.WrapError("sum_type: "+err.Error(), val)
		}
		types = append(types, named)
	}
	switch len(types) {
	case 0:
		return s.sctx.WrapError("sum_type: empty map", val)
	case 1:
		// A union of one type is just that type.
		return s.sctx.LookupTypeValue(types[0])
	}
	return s.sctx.LookupTypeValue(s.sctx.MustLookupTypeUnion(types))
}
