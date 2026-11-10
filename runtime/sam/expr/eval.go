package expr

import (
	"bytes"
	"cmp"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/sam/expr/coerce"
	"github.com/superdb/super/scode"
)

type Evaluator interface {
	Eval(super.Value) super.Value
}

type Function interface {
	Call([]super.Value) super.Value
}

// EvalBool evaluates e with this and returns the result if it is a bool or error.
// Otherwise, EvalBool returns an error.
func EvalBool(sctx *super.Context, this super.Value, e Evaluator) super.Value {
	val := e.Eval(this)
	if val := val.Under(); val.Type() == super.TypeBool || val.IsNull() || val.IsError() {
		return val
	}
	return sctx.WrapError("not type bool", val)
}

func compareNumbers(a, b super.Value, aid, bid int) int {
	switch {
	case super.IsFloat(aid):
		return cmp.Compare(a.Float(), toFloat(b))
	case super.IsFloat(bid):
		return cmp.Compare(toFloat(a), b.Float())
	case super.IsSigned(aid):
		av := a.Int()
		if super.IsUnsigned(bid) {
			if av < 0 {
				return -1
			}
			return cmp.Compare(uint64(av), b.Uint())
		}
		return cmp.Compare(av, b.Int())
	case super.IsSigned(bid):
		bv := b.Int()
		if super.IsUnsigned(aid) {
			if bv < 0 {
				return 1
			}
			return cmp.Compare(a.Uint(), uint64(bv))
		}
		return cmp.Compare(a.Int(), bv)
	}
	return cmp.Compare(a.Uint(), b.Uint())
}

func toFloat(val super.Value) float64 { return coerce.ToNumeric[float64](val) }

func getNthFromRecord(typ *super.TypeRecord, container scode.Bytes, idx int) (scode.Bytes, int) {
	if idx < 0 {
		idx += len(typ.Fields)
		if idx < 0 || idx >= len(typ.Fields) {
			return nil, -1
		}
	}
	it := container.Iter()
	for i := 0; !it.Done(); i++ {
		elem := it.Next()
		if i == idx {
			return elem, idx
		}
	}
	return nil, -1
}

func lookupKey(mapBytes, target scode.Bytes) (scode.Bytes, bool) {
	for it := mapBytes.Iter(); !it.Done(); {
		key := it.Next()
		val := it.Next()
		if bytes.Equal(key, target) {
			return val, true
		}
	}
	return nil, false
}

func indexMap(sctx *super.Context, typ *super.TypeMap, mapBytes scode.Bytes, key super.Value) super.Value {
	if key.IsPlainNone() {
		return key
	}
	if key.Type() != typ.KeyType {
		if union, ok := super.TypeUnder(typ.KeyType).(*super.TypeUnion); ok {
			if tag := union.TagOf(key.Type()); tag >= 0 {
				var b scode.Builder
				super.BuildUnion(&b, union.TagOf(key.Type()), key.Bytes())
				if valBytes, ok := lookupKey(mapBytes, b.Bytes().Body()); ok {
					return deunion(typ.ValType, valBytes)
				}
			}
		}
		return super.None
	}
	if valBytes, ok := lookupKey(mapBytes, key.Bytes()); ok {
		return deunion(typ.ValType, valBytes)
	}
	return super.None
}

func deunion(typ super.Type, b scode.Bytes) super.Value {
	if union, ok := typ.(*super.TypeUnion); ok {
		typ, b = union.Untag(b)
	}
	return super.NewValue(typ, b)
}
