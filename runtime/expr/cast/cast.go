package cast

import (
	"github.com/superdb/super"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

func To(sctx *super.Context, vec vector.Any, typ super.Type) vector.Any {
	if named, ok := typ.(*super.TypeNamed); ok {
		return castNamed(sctx, vec, named)
	}
	vec = vector.Under(vec)
	switch vec.Kind() {
	case vector.KindNull:
		return vector.NewUnionOfOne(sctx.Nullable(typ), vector.NewNull(vec.Len()))
	case vector.KindMissing:
		if typ == super.TypeMissing {
			return vector.NewMissing(vec.Len())
		}
		optionType, ok := typ.(*super.TypeOption)
		if !ok {
			optionType = sctx.LookupTypeOption(typ)
		}
		return vector.NewNone(optionType, vec.Len())
	case vector.KindError:
		return vec
	}
	var c caster
	id := typ.ID()
	if super.IsNumber(id) {
		c = func(vec vector.Any, index []uint32) (vector.Any, []uint32, string, bool) {
			return castToNumber(vec, typ, index)
		}
	} else {
		switch id {
		case super.IDBool:
			c = castToBool
		case super.IDString:
			c = castToString
		case super.IDBytes:
			c = castToBytes
		case super.IDIP:
			c = castToIP
		case super.IDNet:
			c = castToNet
		case super.IDType:
			c = func(vec vector.Any, index []uint32) (vector.Any, []uint32, string, bool) {
				return castToType(sctx, vec, index)
			}
		default:
			return errCastFailed(sctx, vec, typ, "")
		}
	}
	return assemble(sctx, vec, typ, c)
}

type caster func(vector.Any, []uint32) (vector.Any, []uint32, string, bool)

func assemble(sctx *super.Context, vec vector.Any, typ super.Type, fn caster) vector.Any {
	var out vector.Any
	var errs []uint32
	var errMsg string
	var ok bool
	switch vec := vec.(type) {
	case *vector.Const:
		out, errs, errMsg, ok = fn(vec.Any, nil)
		if !ok || len(errs) > 0 {
			return errCastFailed(sctx, vec, typ, errMsg)
		}
		return vector.NewConst(out, vec.Len())
	case *vector.View:
		out, errs, errMsg, ok = fn(vec.Any, vec.Index)
	case *vector.Dict:
		out, errs, errMsg, ok = fn(vec.Any, nil)
		if ok {
			if len(errs) > 0 {
				index, counts, nerrs := vec.RebuildDropTags(errs...)
				errs = nerrs
				out = vector.NewDict(out, index, counts)
			} else {
				out = vector.NewDict(out, vec.Index, vec.Counts)
			}
		}
	default:
		out, errs, errMsg, ok = fn(vec, nil)
	}
	if !ok {
		return errCastFailed(sctx, vec, typ, errMsg)
	}
	if len(errs) > 0 {
		return vector.Combine(out, errs, errCastFailed(sctx, vector.Pick(vec, errs), typ, errMsg))
	}
	return out
}

func castNamed(sctx *super.Context, vec vector.Any, named *super.TypeNamed) vector.Any {
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		vec := vecs[0]
		if vec.Kind() == vector.KindError {
			return vec
		}
		return vector.NewNamed(named, vec)
	}, To(sctx, vec, named.Type))
}

func errCastFailed(sctx *super.Context, vec vector.Any, typ super.Type, msgSuffix string) vector.Any {
	msg := "cannot cast to " + sup.FormatType(typ)
	if msgSuffix != "" {
		msg = msg + ": " + msgSuffix
	}
	return vector.NewWrappedError(sctx, msg, vec)
}

func lengthOf(vec vector.Any, index []uint32) uint32 {
	if index != nil {
		return uint32(len(index))
	}
	return vec.Len()
}
