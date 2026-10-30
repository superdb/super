package expr

import (
	"fmt"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr/cast"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

func NewLiteralCast(sctx *super.Context, expr Evaluator, literal *Literal) (Evaluator, error) {
	typeVal := literal.val
	switch typeVal.Type().ID() {
	case super.IDType:
		typ, err := sctx.LookupByValue(typeVal.Bytes())
		if err != nil {
			return nil, err
		}
		// Casting to any yields fusion(all) values.
		typ = sctx.AnyToFusion(typ)
		if typ.ID() >= super.IDTypeComplex {
			return &casterComplex{sctx, NewSuperDefuse(sctx), expr, typ}, nil
		}
		return &casterPrimitive{sctx, NewSuperDefuse(sctx), expr, typ}, nil
	case super.IDString:
		name := super.DecodeString(typeVal.Bytes())
		if _, err := super.NewContext().LookupTypeNamed(name, super.TypeNull); err != nil {
			return nil, err
		}
		return &casterNamedType{sctx, NewSuperDefuse(sctx), expr, name}, nil
	default:
		return nil, fmt.Errorf("cast type argument is not a type: %s", sup.FormatValue(typeVal))
	}
}

type casterPrimitive struct {
	sctx   *super.Context
	defuse *SuperDefuse
	expr   Evaluator
	typ    super.Type
}

func (c *casterPrimitive) Eval(this vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		return cast.To(c.sctx, vecs[0], c.typ)
	}, c.defuse.Eval(c.expr.Eval(this)))
}

type casterComplex struct {
	sctx   *super.Context
	defuse *SuperDefuse
	expr   Evaluator
	typ    super.Type
}

func (c *casterComplex) Eval(this vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		if vecs[0].Kind() == vector.KindError {
			return vecs[0]
		}
		return cast.Cast(c.sctx, vecs[0], c.typ)
	}, c.defuse.Eval(c.expr.Eval(this)))
}

type casterNamedType struct {
	sctx   *super.Context
	defuse *SuperDefuse
	expr   Evaluator
	name   string
}

func (c *casterNamedType) Eval(this vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipUnions, c.eval, c.defuse.Eval(c.expr.Eval(this)))
}

func (c *casterNamedType) eval(vecs ...vector.Any) vector.Any {
	return CastToNamed(c.sctx, vecs[0], c.name)
}

// CastToNamed casts vec, whose values are of a single type, to the named
// type name with vec's underlying type.
func CastToNamed(sctx *super.Context, vec vector.Any, name string) vector.Any {
	if vec.Kind() == vector.KindError {
		return vec
	}
	vec = vector.Under(vec)
	named, err := sctx.LookupTypeNamed(name, vec.Type())
	if err != nil {
		return vector.NewStringError(sctx, err.Error(), vec.Len())
	}
	return vector.NewNamed(named, vec)
}
