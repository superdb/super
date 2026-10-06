package exec

import (
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// Query runs a flowgraph and gracefully tears down the flowgraph when
// the end of query is reached.
type Query struct {
	vio.Puller
	vio.Meter
	rctx *runtime.Context
}

var _ runtime.Query = (*Query)(nil)

func NewQuery(rctx *runtime.Context, puller vio.Puller, meter vio.Meter) *Query {
	return &Query{
		Puller: puller,
		Meter:  meter,
		rctx:   rctx,
	}
}

func (q *Query) Pull(done bool) (vector.Any, error) {
	if done {
		q.rctx.Cancel()
	}
	return q.Puller.Pull(done)
}
