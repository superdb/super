package function

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

type HasError struct {
	sctx *super.Context
}

func NewHasError(sctx *super.Context) expr.Function {
	return &HasError{sctx}
}

func (h HasError) Call(args ...vector.Any) vector.Any {
	return h.hasError(args[0])
}

func (h HasError) hasError(in vector.Any) vector.Any {
	var index []uint32
	vec := vector.Under(in)
	if view, ok := in.(*vector.View); ok {
		index = view.Index
		vec = view.Any
	}
	switch vec := vec.(type) {
	case *vector.Record:
		var result vector.Any
		for _, f := range vec.Fields {
			if index != nil {
				f = vector.Pick(f, index)
			}
			if result == nil {
				result = h.hasError(f)
				continue
			}
			result = expr.EvalOr(nil, result, h.hasError(f))
		}
		if result == nil {
			return vector.NewFalse(vec.Len())
		}
		return result
	case *vector.Array:
		return listHasError(h.hasError(vec.Values), index, vec.Offsets)
	case *vector.Set:
		return listHasError(h.hasError(vec.Values), index, vec.Offsets)
	case *vector.Map:
		keys := listHasError(h.hasError(vec.Keys), index, vec.Offsets)
		vals := listHasError(h.hasError(vec.Values), index, vec.Offsets)
		return expr.EvalOr(nil, keys, vals)
	default:
		return vector.Apply(vector.ApplyRipUnions|vector.ApplyRipFusions, IsErr{}.Call, in)
	}
}

func listHasError(inner vector.Any, index, offsets []uint32) vector.Any {
	// XXX This is basically the same logic in search.evalForList we should
	// probably centralize this functionality.
	var index2 []uint32
	out := vector.NewFalse(uint32(len(offsets) - 1))
	for i := range out.Len() {
		idx := i
		if index != nil {
			idx = index[i]
		}
		start, end := offsets[idx], offsets[idx+1]
		n := end - start
		if n == 0 {
			continue
		}
		// Reusing index2 across calls here is safe because view does not
		// escape this loop body.
		index2 = slices.Grow(index2[:0], int(n))[:n]
		for k := range n {
			index2[k] = k + start
		}
		view := vector.Pick(inner, index2)
		if expr.FlattenBool(view).Bits.TrueCount() > 0 {
			out.Set(i)
		}
	}
	return out
}

type Is struct {
	sctx *super.Context
}

func (*Is) ApplyOpt() vector.ApplyOpt { return vector.ApplyNone }

func (i *Is) Call(args ...vector.Any) vector.Any {
	vec := args[0]
	typeVec := args[1]
	if len(args) == 3 {
		vec = args[1]
		typeVec = args[2]
	}
	if typeVec.Type().ID() != super.IDType {
		return vector.NewWrappedError(i.sctx, "is: type value argument expected", typeVec)
	}
	if _, ok := typeVec.(*vector.Const); ok {
		typ := vector.TypeValueValue(typeVec, 0)
		v := typ == vec.Type()
		return vector.NewConstBool(v, vec.Len())
	}
	inTyp := vec.Type()
	out := vector.NewFalse(vec.Len())
	for k := range vec.Len() {
		typ := vector.TypeValueValue(typeVec, k)
		if typ == inTyp {
			out.Set(k)
		}
	}
	return out
}

type IsErr struct{}

func (IsErr) Call(args ...vector.Any) vector.Any {
	v := args[0].Kind() == vector.KindError
	return vector.NewConstBool(v, args[0].Len())
}

type NameOf struct {
	sctx *super.Context
}

func (n *NameOf) Call(args ...vector.Any) vector.Any {
	vec := args[0]
	typ := vec.Type()
	if named, ok := typ.(*super.TypeNamed); ok {
		return vector.NewConstString(named.Name, vec.Len())
	}
	if typ.ID() != super.IDType {
		return vector.NewWrappedError(n.sctx, "nameof: not a type", vec)
	}
	out := vector.NewStringEmpty(vec.Len())
	var errs []uint32
	for i := range vec.Len() {
		typ := vector.TypeValueValue(vec, i)
		if named, ok := typ.(*super.TypeNamed); ok {
			out.Append(named.Name)
		} else {
			errs = append(errs, i)
		}
	}
	if len(errs) > 0 {
		return vector.NewCombinedError(n.sctx, "nameof: not a named type", out, vec, errs)
	}
	return out
}

type TypeOf struct {
	sctx *super.Context
}

func (t *TypeOf) ApplyOpt() vector.ApplyOpt { return vector.ApplyNone }

func (t *TypeOf) Call(args ...vector.Any) vector.Any {
	return vector.NewConstType(t.sctx, args[0].Type(), args[0].Len())
}

type TypeName struct {
	sctx *super.Context
}

func (t *TypeName) Call(args ...vector.Any) vector.Any {
	vec := vector.Under(args[0])
	if vec.Type() != super.TypeString {
		return vector.NewWrappedError(t.sctx, "typename: argument must be a string", args[0])
	}
	var errs []uint32
	out := vector.NewTypeValueEmpty()
	for i := range vec.Len() {
		s := vector.StringValue(vec, i)
		if typ := t.sctx.LookupByName(s); typ == nil {
			errs = append(errs, i)
		} else {
			out.Append(typ)
		}
	}
	if len(errs) > 0 {
		return vector.NewCombinedError(t.sctx, "typename: unknown type name", out, vec, errs)
	}
	return out
}

type Error struct {
	sctx *super.Context
}

func (e *Error) Call(args ...vector.Any) vector.Any {
	vec := args[0]
	return vector.NewError(e.sctx.LookupTypeError(vec.Type()), vec)
}

func (e *Error) ApplyOpt() vector.ApplyOpt { return vector.ApplyNone }

type Kind struct {
	sctx *super.Context
}

func NewKind(sctx *super.Context) *Kind {
	return &Kind{sctx}
}

func (k *Kind) Call(args ...vector.Any) vector.Any {
	vec := vector.Under(args[0])
	switch vec.Kind() {
	case vector.KindType:
		out := vector.NewStringEmpty(vec.Len())
		for i := range vec.Len() {
			typ := vector.TypeValueValue(vec, i)
			out.Append(typ.Kind().String())
		}
		return out
	case vector.KindFusion:
		return k.Call(vector.PushView(vec).(*vector.Fusion).Subtypes)
	default:
		return vector.NewConstString(vec.Type().Kind().String(), vec.Len())
	}
}

func (*Kind) ApplyOpt() vector.ApplyOpt { return vector.ApplyNone }

type Defuse struct {
	sctx *super.Context
}

func (u *Defuse) Call(args ...vector.Any) vector.Any {
	return expr.Defuse(u.sctx, args[0])
}
