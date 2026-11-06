package function

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

type downcast struct {
	downcast *expr.Downcast
	defuse   *expr.SuperDefuse
}

func (d *downcast) ApplyOpt() vector.ApplyOpt { return vector.ApplyNone }

// NoDefuse is true since downcast operates on fusion values (e.g., to downcast
// a value to one of its fusion's subtypes).
func (*downcast) NoDefuse() bool { return true }

func newDowncast(sctx *super.Context) *downcast {
	return &downcast{downcast: expr.NewDowncast(sctx), defuse: expr.NewSuperDefuse(sctx)}
}

func (d *downcast) Call(vecs ...vector.Any) vector.Any {
	// Only the value is left fused (see NoDefuse) so the type is defused.
	return d.downcast.To(vecs[0], d.defuse.Eval(vecs[1]))
}
