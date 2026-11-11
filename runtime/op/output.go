package op

import (
	"context"

	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// Output implements the output operator, which may appear in a leaf of a
// query's flowgraph or of a subquery's.  It sends each vector from its parent
// to the query's outputs labeled with the output's name and, as a leaf,
// returns no values.  In a leaf of a query, it's pulled by the mux (see Mux);
// in a subquery, it's pulled by the subquery's evaluation.  It drops any plain
// none values, which come from an expression like ok() or ?. that handles a
// none (see dropPlainNones).  A none of a named type is a value and is output
// as is.  If outputs is nil (e.g., when an expression is evaluated at compile
// time), the values are discarded.
type Output struct {
	ctx     context.Context
	parent  vio.Puller
	outputs *Outputs
	label   string
}

func NewOutput(ctx context.Context, parent vio.Puller, outputs *Outputs, label string) *Output {
	outputs.addLabel(label)
	return &Output{ctx: ctx, parent: parent, outputs: outputs, label: label}
}

func (o *Output) Pull(done bool) (vector.Any, error) {
	if done {
		return o.parent.Pull(true)
	}
	for {
		vec, err := o.parent.Pull(false)
		if vec == nil || err != nil {
			return nil, err
		}
		if vec = dropPlainNones(vec); vec != nil {
			if err := o.outputs.send(o.ctx, o.label, vec); err != nil {
				return nil, err
			}
		}
	}
}
