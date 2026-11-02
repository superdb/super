package function

import (
	"errors"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

// typeCons is a type constructor function, which returns a new type value
// formed from the type values in its argument, e.g., record_type({x:<int64>})
// is <{x:int64}>.
type typeCons struct {
	sctx *super.Context
	name string
	// arg describes the argument for error messages.
	arg string
	// ok returns true if a value of type typ is a valid argument.
	ok func(typ super.Type) bool
	// make returns the type for the argument val.
	make func(val super.Value) (super.Type, error)
}

func (t *typeCons) Call(args ...vector.Any) vector.Any {
	vec := args[0]
	if vec, ok := expr.Catch(t.sctx, expr.CatchAll, vec, t.name); ok {
		return vec
	}
	if !t.ok(vec.Type()) {
		return vector.NewWrappedError(t.sctx, t.name+": argument must be "+t.arg, vec)
	}
	var sb scode.Builder
	if c, ok := vec.(*vector.Const); ok {
		return vector.NewConstFromValue(t.sctx, t.call(&sb, c, 0), c.Len())
	}
	// The same argument is often repeated so each is computed once.
	cache := make(map[string]super.Value)
	b := vector.NewDynamicValueBuilder()
	for i := range vec.Len() {
		val := vector.ValueAt(&sb, vec, i)
		out, ok := cache[string(val.Bytes())]
		if !ok {
			out = t.call(&sb, vec, i).Copy()
			cache[string(val.Bytes())] = out
		}
		b.Write(out)
	}
	return b.Build(t.sctx)
}

func (t *typeCons) call(sb *scode.Builder, vec vector.Any, slot uint32) super.Value {
	val := vector.ValueAt(sb, vec, slot)
	typ, err := t.make(val)
	if err != nil {
		return t.sctx.WrapError(t.name+": "+err.Error(), val)
	}
	return t.sctx.LookupTypeValue(typ)
}

var errNullType = errors.New("null type value")

// lookupType returns the type value in b.
func lookupType(sctx *super.Context, b scode.Bytes) (super.Type, error) {
	if b == nil {
		return nil, errNullType
	}
	return sctx.LookupByValue(b)
}

func isTypeType(typ super.Type) bool {
	return super.TypeUnder(typ) == super.TypeType
}

// isListOf returns true if typ is an array of elem (or an empty array).
func isListOf(typ super.Type, elem super.Type) bool {
	array, ok := super.TypeUnder(typ).(*super.TypeArray)
	if !ok {
		return false
	}
	inner := super.TypeUnder(array.Type)
	return inner == elem || inner == super.TypeNone
}

// listTypes returns the type values in the array val.
func listTypes(sctx *super.Context, val super.Value) ([]super.Type, error) {
	var types []super.Type
	for it := val.Bytes().Iter(); !it.Done(); {
		typ, err := lookupType(sctx, it.Next())
		if err != nil {
			return nil, err
		}
		types = append(types, typ)
	}
	return types, nil
}

// newTypeCons returns the type constructor function name, which takes a
// single argument, or nil if there is no such function.
func newTypeCons(sctx *super.Context, name string) *typeCons {
	t := &typeCons{sctx: sctx, name: name}
	switch name {
	case "array_type", "set_type", "error_type", "fusion_type", "option_type":
		t.arg = "a type value"
		t.ok = isTypeType
		t.make = func(val super.Value) (super.Type, error) {
			inner, err := lookupType(sctx, val.Bytes())
			if err != nil {
				return nil, err
			}
			switch name {
			case "array_type":
				return sctx.LookupTypeArray(inner), nil
			case "set_type":
				return sctx.LookupTypeSet(inner), nil
			case "error_type":
				return sctx.LookupTypeError(inner), nil
			case "fusion_type":
				return sctx.LookupTypeFusion(inner)
			default:
				if _, ok := inner.(*super.TypeOption); ok || inner == super.TypeMissing || inner == super.TypeNone {
					return nil, errors.New("invalid option type")
				}
				return sctx.LookupTypeOption(inner), nil
			}
		}
	case "record_type":
		t.arg = "a record of type values"
		t.ok = func(typ super.Type) bool {
			rec, ok := super.TypeUnder(typ).(*super.TypeRecord)
			return ok && !slices.ContainsFunc(rec.Fields, func(f super.Field) bool {
				return !isTypeType(f.Type)
			})
		}
		t.make = func(val super.Value) (super.Type, error) {
			rec := super.TypeUnder(val.Type()).(*super.TypeRecord)
			fields := make([]super.Field, 0, len(rec.Fields))
			it := val.Bytes().Iter()
			for _, f := range rec.Fields {
				typ, err := lookupType(sctx, it.Next())
				if err != nil {
					return nil, err
				}
				fields = append(fields, super.NewField(f.Name, typ))
			}
			return sctx.LookupTypeRecord(fields)
		}
	case "map_type":
		t.arg = "a record of the form {key:<type>,value:<type>}"
		t.ok = func(typ super.Type) bool {
			rec, ok := super.TypeUnder(typ).(*super.TypeRecord)
			return ok && len(rec.Fields) == 2 &&
				rec.Fields[0].Name == "key" && isTypeType(rec.Fields[0].Type) &&
				rec.Fields[1].Name == "value" && isTypeType(rec.Fields[1].Type)
		}
		t.make = func(val super.Value) (super.Type, error) {
			it := val.Bytes().Iter()
			key, err := lookupType(sctx, it.Next())
			if err != nil {
				return nil, err
			}
			value, err := lookupType(sctx, it.Next())
			if err != nil {
				return nil, err
			}
			return sctx.LookupTypeMap(key, value), nil
		}
	case "union_type":
		t.arg = "an array of type values"
		t.ok = func(typ super.Type) bool { return isListOf(typ, super.TypeType) }
		t.make = func(val super.Value) (super.Type, error) {
			types, err := listTypes(sctx, val)
			if err != nil {
				return nil, err
			}
			// The members of a union in the array are members of the
			// result since a union can't contain a union.
			var flat []super.Type
			for _, typ := range types {
				if union, ok := typ.(*super.TypeUnion); ok {
					flat = append(flat, union.Types...)
				} else {
					flat = append(flat, typ)
				}
			}
			flat = super.UniqueTypes(flat)
			switch len(flat) {
			case 0:
				return nil, errors.New("empty array")
			case 1:
				// A union of one type is just that type.
				return flat[0], nil
			}
			union, ok := sctx.LookupTypeUnion(flat)
			if !ok {
				return nil, errors.New("invalid union type")
			}
			return union, nil
		}
	case "enum_type":
		t.arg = "an array of strings"
		t.ok = func(typ super.Type) bool { return isListOf(typ, super.TypeString) }
		t.make = func(val super.Value) (super.Type, error) {
			var symbols []string
			for it := val.Bytes().Iter(); !it.Done(); {
				b := it.Next()
				if b == nil {
					return nil, errors.New("null symbol")
				}
				s := super.DecodeString(b)
				if slices.Contains(symbols, s) {
					return nil, errors.New("duplicate symbol " + s)
				}
				symbols = append(symbols, s)
			}
			if len(symbols) == 0 {
				return nil, errors.New("empty array")
			}
			return sctx.LookupTypeEnum(symbols), nil
		}
	default:
		return nil
	}
	return t
}

// namedType is the named_type function, which returns a named type from its
// name and type arguments.
type namedType struct {
	sctx *super.Context
}

func (n *namedType) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.CatchFirst(n.sctx, expr.CatchAll, args, "named_type"); ok {
		return vec
	}
	names, types := args[0], args[1]
	if super.TypeUnder(names.Type()) != super.TypeString {
		return vector.NewWrappedError(n.sctx, "named_type: name must be a string", names)
	}
	if !isTypeType(types.Type()) {
		return vector.NewWrappedError(n.sctx, "named_type: type must be a type value", types)
	}
	var sb scode.Builder
	b := vector.NewDynamicValueBuilder()
	for i := range names.Len() {
		name := vector.StringValue(names, i)
		val := vector.ValueAt(&sb, types, i)
		typ, err := lookupType(n.sctx, val.Bytes())
		if err == nil {
			var named *super.TypeNamed
			if named, err = n.sctx.LookupTypeNamed(name, typ); err == nil {
				b.Write(n.sctx.LookupTypeValue(named))
				continue
			}
		}
		b.Write(n.sctx.WrapError("named_type: "+err.Error(), val))
	}
	return b.Build(n.sctx)
}
