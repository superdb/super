package function

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

// Optionize returns its type argument with every union type that has a
// missing member replaced by an option type (see super.Context.OptionizeMissing).
type Optionize struct {
	sctx *super.Context
}

func (o *Optionize) Call(args ...vector.Any) vector.Any {
	vec := args[0]
	switch vec.Kind() {
	case vector.KindError, vector.KindNull:
		return vec
	}
	if vec.Type() != super.TypeType {
		return vector.NewWrappedError(o.sctx, "optionize: argument is not a type", vec)
	}
	if c, ok := vec.(*vector.Const); ok {
		typ := o.sctx.OptionizeMissing(vector.TypeValueValue(c, 0))
		return vector.NewConst(vector.NewTypeValue([]super.Type{typ}), c.Len())
	}
	types := make([]super.Type, vec.Len())
	for i := range types {
		types[i] = o.sctx.OptionizeMissing(vector.TypeValueValue(vec, uint32(i)))
	}
	return vector.NewTypeValue(types)
}
