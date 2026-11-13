package op

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type cachedSubquery struct {
	sctx   *super.Context
	body   vio.Puller
	sql    bool
	cached vector.Any
}

// NewCachedSubquery returns an uncorrelated subquery, which is evaluated once.
// If it produces no values, its value is null if sql is true (as in SQL) and
// otherwise none.
func NewCachedSubquery(sctx *super.Context, body vio.Puller, sql bool) expr.Evaluator {
	return &cachedSubquery{sctx: sctx, body: body, sql: sql}
}

func (c *cachedSubquery) Eval(this vector.Any) vector.Any {
	if c.cached == nil {
		c.cached = subqueryBodyPull(c.sctx, c.body, c.sql)
	}
	index := make([]uint32, this.Len())
	return vector.Pick(c.cached, index)
}
