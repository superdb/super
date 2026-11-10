package function

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

// Optionize returns its type argument with every union type that has a
// none member, and every union type of a record field that has a null
// member, replaced by an option type (see super.Context.OptionizeNone).
type Optionize struct {
	sctx *super.Context
}

func (o *Optionize) Call(args ...vector.Any) vector.Any {
	vec := args[0]
	if vec, ok := expr.Catch(o.sctx, expr.CatchAll, vec, "optionize"); ok {
		return vec
	}
	if vec.Type() != super.TypeType {
		return vector.NewWrappedError(o.sctx, "optionize: argument is not a type", vec)
	}
	if c, ok := vec.(*vector.Const); ok {
		typ := o.sctx.OptionizeNone(vector.TypeValueValue(c, 0))
		return vector.NewConst(vector.NewTypeValue(o.sctx, []super.Type{typ}), c.Len())
	}
	types := make([]super.Type, vec.Len())
	for i := range types {
		types[i] = o.sctx.OptionizeNone(vector.TypeValueValue(vec, uint32(i)))
	}
	return vector.NewTypeValue(o.sctx, types)
}
