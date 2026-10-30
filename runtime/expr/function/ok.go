package function

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

type Ok struct {
	defuse *expr.SuperDefuse
}

func newOk(sctx *super.Context) *Ok {
	return &Ok{expr.NewSuperDefuse(sctx)}
}

func (o *Ok) Call(args ...vector.Any) vector.Any {
	return vector.Apply(vector.ApplyNone, o.call, o.defuse.Eval(args[0]))
}

func (o *Ok) call(args ...vector.Any) vector.Any {
	vec := args[0]
	if vec.Kind() == vector.KindError {
		return vector.NewNone(vec.Len())
	}
	return vec
}

func (*Ok) ApplyOpt() vector.ApplyOpt { return vector.ApplyNone }
