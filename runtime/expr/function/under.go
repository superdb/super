package function

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

type Under struct {
	sctx *super.Context
}

// ApplyOpt keeps unions from being ripped before Call, which handles them, so
// under strips one level from a named union (e.g., a named sum type) rather
// than from each of the union's values.
func (*Under) ApplyOpt() vector.ApplyOpt { return vector.ApplyNone }

func (u *Under) Call(args ...vector.Any) vector.Any {
	vec := args[0]
	if _, ok := expr.Catch(u.sctx, expr.CatchNone, args[0], "under"); ok {
		return vec
	}
	var index []uint32
	switch vec2 := vec.(type) {
	case *vector.Const:
		return vector.NewConst(u.Call(vec2.Any), vec.Len())
	case *vector.View:
		vec = vec2.Any
		index = vec2.Index
	}
	var out vector.Any
	switch vec := vec.(type) {
	case *vector.Named:
		out = vec.Any
	case *vector.Error:
		out = vec.Vals
	case *vector.Option:
		out = vector.Apply(vector.ApplyRipOptions, func(vecs ...vector.Any) vector.Any {
			return vecs[0].(*vector.Option).Any
		}, vec)
	case *vector.Union:
		out = vec.Dynamic()
	case *vector.TypeOnly:
		// A type-only vector (see typeDemand in the optimizer) never has a
		// named, error, option, or union type since the loader builds those
		// vectors with their tags around type-only values, so under leaves
		// it as is just as it would its data.
		return args[0]
	case *vector.TypeValue:
		b := vector.NewDynamicValueBuilder()
		for i := range vec.Len() {
			b.Write(u.underType(vec.Value(i)))
		}
		out = b.Build(u.sctx)
	default:
		return args[0]
	}
	if index != nil {
		return vector.Pick(out, index)
	}
	return out
}

// underType returns the type underlying typ as a value: for a named type,
// the named type's underlying type; for an array or set type, the element
// type; for an option, error, or fusion type, the type it wraps; for a union
// type, an array of the union's types; for a map type, a record
// {key:<K>,value:<V>} of the key and value types; and otherwise, typ.
func (u *Under) underType(typ super.Type) super.Value {
	switch t := typ.(type) {
	case *super.TypeNamed:
		return u.sctx.LookupTypeValue(super.TypeUnder(t))
	case *super.TypeArray:
		return u.sctx.LookupTypeValue(t.Type)
	case *super.TypeSet:
		return u.sctx.LookupTypeValue(t.Type)
	case *super.TypeOption:
		return u.sctx.LookupTypeValue(t.Type)
	case *super.TypeError:
		return u.sctx.LookupTypeValue(t.Type)
	case *super.TypeFusion:
		return u.sctx.LookupTypeValue(t.Type)
	case *super.TypeUnion:
		var b scode.Builder
		for _, typ := range t.Types {
			b.Append(u.sctx.LookupTypeValue(typ).Bytes())
		}
		return super.NewValue(u.sctx.LookupTypeArray(super.TypeType), b.Bytes())
	case *super.TypeMap:
		rec := u.sctx.MustLookupTypeRecord([]super.Field{
			super.NewField("key", super.TypeType),
			super.NewField("value", super.TypeType),
		})
		var b scode.Builder
		b.Append(u.sctx.LookupTypeValue(t.KeyType).Bytes())
		b.Append(u.sctx.LookupTypeValue(t.ValType).Bytes())
		return super.NewValue(rec, b.Bytes())
	}
	return u.sctx.LookupTypeValue(typ)
}
