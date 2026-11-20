package function

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	samfunc "github.com/superdb/super/runtime/sam/expr/function"
	"github.com/superdb/super/vector"
)

type downcast struct {
	sctx      *super.Context
	downcast  *expr.Downcast
	defuse    *expr.SuperDefuse
	hasFusion samfunc.FusionChecker
}

func (d *downcast) ApplyOpt() vector.ApplyOpt { return vector.ApplyNone }

// NoDefuse is true so that downcast sees a fusion value and returns an error
// for it rather than downcasting its defused value.  A fusion value is turned
// into its subtype's value by defuse, not downcast.
func (*downcast) NoDefuse() bool { return true }

func newDowncast(sctx *super.Context) *downcast {
	return &downcast{sctx: sctx, downcast: expr.NewDowncast(sctx), defuse: expr.NewSuperDefuse(sctx)}
}

func (d *downcast) Call(vecs ...vector.Any) vector.Any {
	// Only the value is left fused (see NoDefuse) so the type is defused.
	to := d.defuse.Eval(vecs[1])
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		if d.hasFusion.Check(vecs[0].Type()) {
			return vector.NewWrappedError(d.sctx, "downcast: cannot downcast a fusion value", vecs[0])
		}
		return d.downcast.To(vecs[0], vecs[1])
	}, vecs[0], to)
}
