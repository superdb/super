package expr

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type putter struct {
	sctx   *super.Context
	e      Evaluator
	defuse *SuperDefuse
}

// NewPutter wraps e to implement the behavior of the put operator, which emits
// an error when an input value is not a record.
func NewPutter(sctx *super.Context, e Evaluator) Evaluator {
	return &putter{sctx, e, NewSuperDefuse(sctx)}
}

func (p *putter) Eval(vec vector.Any) vector.Any {
	// XXX At some point we should do something more optimized than calling
	// defuse.Eval but something like this is needed to do puts on fused
	// records while keeping the original shape.
	return vector.Apply(vector.ApplyNone, p.eval, p.defuse.Eval(vec))
}

func (p *putter) eval(vecs ...vector.Any) vector.Any {
	vec := vecs[0]
	switch vec.Kind() {
	case vector.KindRecord:
		return p.e.Eval(vec)
	case vector.KindError:
		return vec
	default:
		return vector.NewWrappedError(p.sctx, "put: not a record", vec)
	}
}
