package expr

import (
	"fmt"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	samfunc "github.com/superdb/super/runtime/sam/expr/function"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

type This struct{}

func (*This) Eval(val vector.Any) vector.Any {
	return val
}

type DotExpr struct {
	sctx      *super.Context
	defuse    *SuperDefuse
	entity    Evaluator
	key       string
	noneish   bool
	nullish   bool
	okPush    bool
	hasFusion samfunc.FusionChecker
}

func NewDotExpr(sctx *super.Context, record Evaluator, field string, noneish, nullish bool) *DotExpr {
	return &DotExpr{
		sctx:    sctx,
		defuse:  NewSuperDefuse(sctx),
		entity:  record,
		key:     field,
		noneish: noneish,
		nullish: nullish,
	}
}

func NewDottedExpr(sctx *super.Context, f field.Chain) Evaluator {
	ret := Evaluator(&This{})
	for _, elem := range f {
		ret = NewDotExpr(sctx, ret, elem.ID, elem.Noneish, elem.Nullish)
	}
	return ret
}

func (d *DotExpr) Eval(vec vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipFusions|vector.ApplyRipUnions|vector.ApplyRipOptions, d.eval, d.entity.Eval(vec))
}

func (d *DotExpr) eval(vecs ...vector.Any) vector.Any {
	vec := vecs[0]
	switch vec := vector.Under(vec).(type) {
	case *vector.Null:
		if d.nullish {
			return vec
		}
	case *vector.Const:
		return vector.NewConst(d.eval(vec.Any), vec.Len())
	case *vector.Missing:
		if d.noneish {
			return vec
		}
		return d.noFieldErr(vec)
	case *vector.Option:
		// This is either a pure some or a pure none since we called
		// Apply with RipOptions.  For option nones with ?. access, the
		// result is a none of the field's type so e?.f keeps the type of
		// e.f, e.g., so that {y:e?.f} has an optional field y.
		if _, ok := vec.Any.(*vector.None); ok {
			if d.noneish {
				return d.noneOfField(vec)
			}
			return d.noneErr(vec)
		}
		return d.eval(vec.Any)
	case *vector.None:
		// A bare none (e.g., from ok()) is handled like an option none.
		if d.noneish {
			return vec
		}
		return d.noneErr(vec)
	case *vector.Record:
		i, ok := vec.Typ.IndexOfField(d.key)
		if !ok {
			if d.nullish {
				return vector.NewNull(vec.Len())
			}
			return d.noFieldErr(vec)
		}
		out := vec.Fields[i]
		if _, ok := out.(*vector.Missing); ok {
			// A missing field (e.g., from a projection of a record type
			// without the field) is the same as no field.  For nullish
			// expressions, turn missing into null.
			if d.nullish {
				return vector.NewNull(vec.Len())
			}
			return d.noFieldErr(vec)
		}
		if !hasUnionMissing(out) {
			return out
		}
		return vector.Apply(vector.ApplyDefuseMissings, func(vecs ...vector.Any) vector.Any {
			unripped := vecs[0]
			ripped := vecs[1]
			if _, ok := ripped.(*vector.Missing); ok {
				if d.nullish {
					return vector.NewNull(ripped.Len())
				}
				return d.noFieldErr(ripped)
			}
			return unripped
		}, vector.AddNoRip(out), out)
	case *vector.TypeValue:
		return d.derefTypeValue(vec)
	case *vector.Map:
		keyVec := vector.NewConstString(d.key, vec.Len())
		return indexMap(d.sctx, vec, keyVec)
	case *vector.View:
		return vector.Pick(d.eval(vec.Any), vec.Index)
	case *vector.Error:
		return vec
	}
	return vector.NewStringError(d.sctx, fmt.Sprintf("cannot access field '%s' of %s", d.key, kindName(vec.Type())), vec.Len())
}

// noneOfField returns a none vector for e?.f where vec is an option none.
// The none is an option of the type of field d.key so e?.f keeps the type
// of e.f.  ?. handles only the none, so it's an error, as it would be for
// a some, if the option's type has no field d.key.
func (d *DotExpr) noneOfField(vec *vector.Option) vector.Any {
	var typ super.Type
	switch t := super.TypeUnder(vec.Typ.Type).(type) {
	case *super.TypeRecord:
		var ok bool
		if typ, ok = t.TypeOfField(d.key); !ok {
			return d.noFieldErr(vec)
		}
	case *super.TypeMap:
		typ = t.ValType
	case *super.TypeUnion, *super.TypeFusion:
		// Which type the none stands in for isn't known.
		return vector.NewNone(vec.Len())
	default:
		return vector.NewStringError(d.sctx, fmt.Sprintf("cannot access field '%s' of %s", d.key, kindName(t)), vec.Len())
	}
	if super.TypeUnder(typ) == super.TypeNone {
		return vector.NewNone(vec.Len())
	}
	if opt, ok := d.sctx.Optionize(typ).(*super.TypeOption); ok {
		return vector.NewOptionNone(opt, vec.Len())
	}
	return vector.NewNone(vec.Len())
}

// derefTypeValue returns the type of field d.key for each type value in vec.
// See derefType.
func (d *DotExpr) derefTypeValue(vec *vector.TypeValue) vector.Any {
	var errs []uint32
	typvals := vector.NewTypeValueEmpty()
	for i := range vec.Len() {
		if typ := derefType(d.sctx, vec.Value(i), d.key); typ != nil {
			typvals.Append(typ)
			continue
		}
		errs = append(errs, i)
	}
	if len(errs) > 0 {
		msg := fmt.Sprintf("no such field '%s'", sup.QuotedName(d.key))
		if len(errs) == int(vec.Len()) {
			return vector.NewWrappedError(d.sctx, msg, vec)
		}
		return vector.NewCombinedError(d.sctx, msg, typvals, vec, errs)
	}
	return typvals
}

// derefType returns the type of field key in typ or nil if there is no
// such field.  The dot navigates through named, option, fusion, and error
// types and into the element types of arrays and sets, so, e.g.,
// <[{a:{b:string}}]>.a.b is <string>.  A map type has no fields since its
// keys aren't known.  For a union, the
// result is the union of the field's types in the union's types that have
// the field.
func derefType(sctx *super.Context, typ super.Type, key string) super.Type {
	switch typ := typ.(type) {
	case *super.TypeNamed:
		return derefType(sctx, typ.Type, key)
	case *super.TypeRecord:
		if t, ok := typ.TypeOfField(key); ok {
			return t
		}
	case *super.TypeArray:
		return derefType(sctx, typ.Type, key)
	case *super.TypeSet:
		return derefType(sctx, typ.Type, key)
	case *super.TypeUnion:
		return unionOfTypes(sctx, typ, func(typ super.Type) super.Type {
			return derefType(sctx, typ, key)
		})
	case *super.TypeFusion:
		return derefType(sctx, typ.Type, key)
	case *super.TypeOption:
		return derefType(sctx, typ.Type, key)
	case *super.TypeError:
		return derefType(sctx, typ.Type, key)
	}
	return nil
}

// unionOfTypes returns the union of the types that f returns for the types of
// union, ignoring nil results, or nil if all of the results are nil.
func unionOfTypes(sctx *super.Context, union *super.TypeUnion, f func(super.Type) super.Type) super.Type {
	var types []super.Type
	for _, t := range union.Types {
		out := f(t)
		if out == nil {
			continue
		}
		if u, ok := out.(*super.TypeUnion); ok {
			types = append(types, u.Types...)
		} else {
			types = append(types, out)
		}
	}
	types = super.UniqueTypes(types)
	switch len(types) {
	case 0:
		return nil
	case 1:
		return types[0]
	}
	return sctx.MustLookupTypeUnion(types)
}

func (d *DotExpr) noneErr(vec vector.Any) *vector.Error {
	return vector.NewStringError(d.sctx, fmt.Sprintf("cannot access field '%s' of none (use ?. or ok())", d.key), vec.Len())
}

func (d *DotExpr) noFieldErr(on vector.Any) *vector.Error {
	return vector.NewStringError(d.sctx, fmt.Sprintf("no such field %s", sup.QuotedName(d.key)), on.Len())
}

func kindName(typ super.Type) string {
	switch t := super.TypeUnder(typ).(type) {
	case *super.TypeRecord:
		return "record"
	case *super.TypeArray:
		return "array"
	case *super.TypeSet:
		return "set"
	case *super.TypeMap:
		return "map"
	case *super.TypeEnum:
		return "enum"
	case *super.TypeUnion:
		return "union"
	case *super.TypeOption:
		return "option"
	default:
		return sup.FormatType(t) // int64, time, ip, null, ...
	}
}

func hasUnionMissing(vec vector.Any) bool {
	switch vec := vec.(type) {
	case *vector.Union:
		return hasMissing(vec.Dynamic())
	case *vector.Fusion:
		return hasUnionMissing(vec.Values)
	case *vector.Dynamic:
		return slices.ContainsFunc(vec.Values, hasUnionMissing)
	case *vector.View:
		return hasUnionMissing(vec.Any)
	}
	return false
}

func hasMissing(vec vector.Any) bool {
	switch vec := vec.(type) {
	case *vector.Missing:
		return vec.Len() > 0
	case *vector.Union:
		return hasMissing(vec.Dynamic())
	case *vector.Fusion:
		return hasMissing(vec.Values)
	case *vector.Dynamic:
		return slices.ContainsFunc(vec.Values, hasMissing)
	}
	return false
}
