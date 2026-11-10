package op

import (
	"context"

	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// Debug passes its input through and sends the values of expr to the query's
// outputs labeled "debug".  If outputs is nil (e.g., when an expression is
// evaluated at compile time), the values are discarded.
type Debug struct {
	parent  vio.Puller
	ctx     context.Context
	expr    expr.Evaluator
	filter  expr.Evaluator
	outputs *Outputs
}

func NewDebug(ctx context.Context, expr expr.Evaluator, filter expr.Evaluator, outputs *Outputs, parent vio.Puller) *Debug {
	return &Debug{
		parent:  parent,
		ctx:     ctx,
		expr:    expr,
		filter:  filter,
		outputs: outputs,
	}
}

func (d *Debug) Pull(done bool) (vector.Any, error) {
	val, err := d.parent.Pull(done)
	if val == nil || d.outputs == nil {
		return val, err
	}
	filtered := val
	if d.filter != nil {
		filtered, _ = applyMask(val, d.filter.Eval(filtered))
	}
	// A plain none is dropped, as for the values operator.
	if e := dropPlainNones(d.expr.Eval(filtered)); e != nil && e.Len() != 0 {
		if err := d.outputs.send(d.ctx, "debug", e); err != nil {
			return nil, err
		}
	}
	return val, err
}
