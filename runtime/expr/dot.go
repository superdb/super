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
	return vector.Apply(vector.ApplyNone, d.eval, d.entity.Eval(vec))
}

func (d *DotExpr) eval(outerVecs ...vector.Any) vector.Any {
	var eval func(...vector.Any) vector.Any
	eval = func(innerVecs ...vector.Any) vector.Any {
		on := innerVecs[0]
		ripped := innerVecs[1]
		switch val := vector.Under(ripped).(type) {
		case *vector.Null:
			if d.nullish {
				return val
			}
		case *vector.Missing:
			// XXX TBD - this should be an error
			return val
		case *vector.Option:
			// This is either a pure some or a pure none since we called
			// Apply with RipOptions.  For option nones with ?. access
			// we just return the none as in its option form.
			if _, ok := val.Any.(*vector.Missing); ok {
				if d.noneish {
					return val
				}
				return vector.NewWrappedError(d.sctx, fmt.Sprintf(".: encountered none dereferencing field %s (consider ?.)", sup.QuotedName(d.key)), val)
			}
			return eval(on, val.Any)
		case *vector.Record:
			i, ok := val.Typ.IndexOfField(d.key)
			if !ok {
				if d.nullish {
					return vector.NewNull(val.Len())
				}
				return vector.NewWrappedError(d.sctx, fmt.Sprintf("no such field %s", sup.QuotedName(d.key)), val)
			}
			out := val.Fields[i]
			if _, ok := out.(*vector.Missing); ok {
				return vector.NewWrappedError(d.sctx, fmt.Sprintf("no such field %s", sup.QuotedName(d.key)), val)
			}
			if !hasUnionMissing(out) {
				return out
			}
			return vector.Apply(vector.ApplyDefuseMissings, func(vecs ...vector.Any) vector.Any {
				on := vecs[0]
				unripped := vecs[1]
				ripped := vecs[2]
				if _, ok := ripped.(*vector.Missing); ok {
					if d.nullish {
						return vector.NewNull(ripped.Len())
					}
					//XXX will check noneish here like nullish above in
					// a susequent PR
					return vector.NewWrappedError(d.sctx, fmt.Sprintf("no such field %s", sup.QuotedName(d.key)), on)
				}
				return unripped
			}, vector.AddNoRip(on), vector.AddNoRip(out), out)
		case *vector.TypeValue:
			var errs []uint32
			typvals := vector.NewTypeValueEmpty()
			for i := range val.Len() {
				typ := val.Value(i)
				if typ, ok := super.TypeUnder(typ).(*super.TypeRecord); ok {
					if typ, ok := typ.TypeOfField(d.key); ok {
						typvals.Append(typ)
						continue
					}
				}
				errs = append(errs, i)
			}
			if len(errs) > 0 {
				return vector.NewCombinedError(d.sctx, fmt.Sprintf("no such field %s", sup.QuotedName(d.key)), typvals, val, errs)
			}
			return typvals
		case *vector.Map:
			keyVec := vector.NewConstString(d.key, val.Len())
			return indexMap(d.sctx, val, keyVec)
		case *vector.View:
			if pushed := vector.PushView(val); pushed != vector.Any(val) {
				return eval(on, pushed)
			}
			// PushView doesn't handle this kind (e.g., TypeValue), so use the
			// underlying vector as its own "on" to keep lengths aligned. Error
			// values on this path wrap the ripped value instead of the outer one.
			return vector.Pick(eval(val.Any, val.Any), val.Index)
		}
		op := "."
		if d.noneish {
			op = "?."
		} else if d.nullish {
			op = "??."
		}
		return vector.NewWrappedError(d.sctx, fmt.Sprintf("'%s': applied to non-record", op), on)
	}
	vec := outerVecs[0]
	return vector.Apply(vector.ApplyRipFusions|vector.ApplyRipUnions|vector.ApplyRipOptions, eval, vector.AddNoRip(vec), vec)
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
