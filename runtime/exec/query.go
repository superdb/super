package exec

import (
	"context"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/parser"
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

//XXX from runtime

func CompileQuery(ctx context.Context, sctx *super.Context, ast *parser.AST, readers []vio.Puller) (Query, error) {
	rctx := runtime.NewContext(ctx, sctx)
	q, err := c.NewQuery(rctx, ast, readers, 0)
	if err != nil {
		rctx.Cancel()
		return nil, err
	}
	return q, nil
}

func CompileQueryForDB(ctx context.Context, sctx *super.Context, ast *parser.AST) (Query, error) {
	rctx := runtime.NewContext(ctx, sctx)
	q, err := c.NewQuery(rctx, ast, nil, 0)
	if err != nil {
		rctx.Cancel()
		return nil, err
	}
	return q, nil
}
