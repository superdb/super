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
	defuse    *Defuse
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
		defuse:  NewDefuse(sctx),
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
		var errs []uint32
		typvals := vector.NewTypeValueEmpty()
		for i := range vec.Len() {
			typ := vec.Value(i)
			if typ, ok := super.TypeUnder(typ).(*super.TypeRecord); ok {
				if typ, ok := typ.TypeOfField(d.key); ok {
					typvals.Append(typ)
					continue
				}
			}
			errs = append(errs, i)
		}
		if len(errs) > 0 {
			return vector.NewCombinedError(d.sctx, fmt.Sprintf("no such field '%s'", sup.QuotedName(d.key)), typvals, vec, errs)
		}
		return typvals
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
