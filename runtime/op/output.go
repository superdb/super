package op

import (
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// Output is the puller for an output of a query.  It drops any plain none
// values, which come from an expression like ok() or ?. that handles a none
// (see dropPlainNones).  A none of a named type is a value and is output
// as is.
type Output struct {
	parent vio.Puller
}

func NewOutput(parent vio.Puller) *Output {
	return &Output{parent: parent}
}

func (o *Output) Pull(done bool) (vector.Any, error) {
	for {
		vec, err := o.parent.Pull(done)
		if vec == nil || err != nil {
			return vec, err
		}
		if vec = dropPlainNones(vec); vec != nil {
			return vec, nil
		}
	}
}
