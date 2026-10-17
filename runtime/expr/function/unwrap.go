package function

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

type Unwrap struct {
	sctx   *super.Context
	defuse *expr.Defuse
}

func newUnwrap(sctx *super.Context) *Unwrap {
	return &Unwrap{sctx, expr.NewDefuse(sctx)}
}

func (u *Unwrap) Call(args ...vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipOptions, u.call, u.defuse.Eval(args[0]))
}

func (u *Unwrap) call(args ...vector.Any) vector.Any {
	vec := args[0]
	if option, ok := vector.Under(vec).(*vector.Option); ok {
		if _, ok := option.Any.(*vector.Missing); ok {
			return vector.NewWrappedError(u.sctx, "unwrap on none", vec)
		}
		return option.Any
	}
	return vec
}

func (*Unwrap) ApplyOpt() vector.ApplyOpt { return vector.ApplyNone }
