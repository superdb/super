package expr

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/vector"
)

// Renamer renames one or more fields in a record.  See [expr.Renamer], on which
// it relies, for more detail.
type Renamer struct {
	sctx    *super.Context
	defuse  *SuperDefuse
	renamer *expr.Renamer
}

func NewRenamer(sctx *super.Context, srcs, dsts []*expr.Lval) *Renamer {
	return &Renamer{sctx, NewSuperDefuse(sctx), expr.NewRenamer(sctx, srcs, dsts)}
}

func (r *Renamer) Eval(vec vector.Any) vector.Any {
	return vector.Apply(vector.ApplyNone, r.eval, r.defuse.Eval(vec))
}

func (r *Renamer) eval(vecs ...vector.Any) vector.Any {
	vec := vecs[0]
	recVec, ok := vector.PushView(vector.Under(vec)).(*vector.Record)
	if !ok {
		return vec
	}
	val, err := r.renamer.EvalToValAndError(super.NewValue(vec.Type(), nil))
	if err != nil {
		return vector.NewWrappedError(r.sctx, err.Error(), vec)
	}
	return recVec.ChangeType(val.Type().(*super.TypeRecord))
}
