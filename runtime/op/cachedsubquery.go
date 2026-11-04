package op

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
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
		c.cached = c.exec()
	}
	index := make([]uint32, this.Len())
	return vector.Pick(c.cached, index)
}

// exec executes c.body and returns a vector of length 1.
func (c *cachedSubquery) exec() vector.Any {
	var vecs []vector.Any
	for {
		vec, err := c.body.Pull(false)
		if err != nil {
			return vector.NewStringError(c.sctx, err.Error(), 1)
		}
		if vec == nil {
			break
		}
		vecs = append(vecs, vec)
	}
	if len(vecs) == 0 {
		return emptyResult(c.sql)
	}
	if len(vecs) == 1 && vecs[0].Len() == 1 {
		return vecs[0]
	}
	return c.newArray(c.vectorConcat(vecs))
}

// newArray returns a vector whose single element is an array of elems.
func (c *cachedSubquery) newArray(elems vector.Any) vector.Any {
	if d, ok := elems.(*vector.Dynamic); ok {
		elems = vector.NewUnionFromDynamic(c.sctx, d)
	}
	arrayType := c.sctx.LookupTypeArray(elems.Type())
	offsets := []uint32{0, elems.Len()}
	return vector.NewArray(arrayType, offsets, elems)
}

// vectorConcat returns a vector concatenating vecs.
func (c *cachedSubquery) vectorConcat(vecs []vector.Any) vector.Any {
	if len(vecs) == 0 {
		return nil
	}
	if len(vecs) == 1 {
		return vecs[0]
	}
	db := vbuild.NewDynamicBuilder()
	for _, vec := range vecs {
		db.Write(vec)
	}
	return db.Build()
}
