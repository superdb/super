package function

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

type Some struct {
	sctx   *super.Context
	defuse *expr.Defuse
}

func newSome(sctx *super.Context) *Some {
	return &Some{sctx, expr.NewDefuse(sctx)}
}

func (s *Some) Call(args ...vector.Any) vector.Any {
	return vector.Apply(vector.ApplyNone, s.call, s.defuse.Eval(args[0]))
}

func (s *Some) call(args ...vector.Any) vector.Any {
	vec := args[0]
	switch vec.Kind() {
	case vector.KindError, vector.KindOption, vector.KindMissing:
		return vec
	default:
		return vector.NewSome(s.sctx.LookupTypeOption(vec.Type()), vec)
	}
}

func (*Some) ApplyOpt() vector.ApplyOpt { return vector.ApplyNone }
