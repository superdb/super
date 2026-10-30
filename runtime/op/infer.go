package op

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/expr/cast"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Infer struct {
	rctx   *runtime.Context
	parent vio.Puller
	limit  int

	converter *converter
	defuse    *expr.SuperDefuse
	needEOS   bool
}

func NewInfer(rctx *runtime.Context, parent vio.Puller, limit int) *Infer {
	return &Infer{
		rctx:   rctx,
		parent: parent,
		limit:  limit,
		defuse: expr.NewSuperDefuse(rctx.Sctx),
	}
}

func (i *Infer) Pull(done bool) (vector.Any, error) {
	if done {
		i.eos()
		return i.parent.Pull(true)
	}
	if i.needEOS {
		i.eos()
		return nil, nil
	}
	if i.converter == nil {
		i.converter = newConverter(i.rctx, i.limit)
	}
	for {
		vec, err := i.parent.Pull(false)
		if err != nil {
			i.eos()
			return nil, err
		}
		if vec == nil {
			vec, err := i.converter.drain(vector.NewDynamicValueBuilder(), true)
			if vec == nil || err != nil {
				i.eos()
				return nil, err
			}
			i.needEOS = true
			return vec, nil
		}
		vec = i.defuse.Eval(vec)
		vec, err = i.converter.process(vec)
		if err != nil {
			i.eos()
			return nil, err
		}
		if vec != nil {
			return vec, nil
		}
	}
}

func (i *Infer) eos() {
	i.converter = nil
	i.needEOS = false
}

type converter struct {
	rctx   *runtime.Context
	queues map[super.Type][]super.Value
	target map[super.Type]super.Type
	limit  int
	sb     scode.Builder
}

func newConverter(rctx *runtime.Context, limit int) *converter {
	return &converter{
		rctx:   rctx,
		queues: make(map[super.Type][]super.Value),
		target: make(map[super.Type]super.Type),
		limit:  limit,
	}
}

func (c *converter) process(vec vector.Any) (vector.Any, error) {
	// Values whose type has a target are converted now while the others
	// are queued until their target is inferred.
	var index []uint32
	for i := range vec.Len() {
		val := vector.ValueAt(&c.sb, vec, i)
		if _, ok := c.target[val.Type()]; ok || c.enq(val) {
			index = append(index, i)
		}
	}
	b := vector.NewDynamicValueBuilder()
	if len(index) > 0 {
		if len(index) != int(vec.Len()) {
			vec = vector.Pick(vec, index)
		}
		c.convert(b, vec)
	}
	return c.drain(b, false)
}

func (c *converter) drain(b *vector.DynamicValueBuilder, force bool) (vector.Any, error) {
	for typ, q := range c.queues {
		// The queues can get big, so we mind the context.
		if err := c.rctx.Err(); err != nil {
			return nil, err
		}
		if force || (c.limit != 0 && len(q) >= c.limit) {
			c.infer(q)
			delete(c.queues, typ)
			qb := vector.NewValueBuilder(typ)
			for _, val := range q {
				qb.Write(val.Bytes())
			}
			c.convert(b, qb.Build(c.rctx.Sctx))
		}
	}
	vec := b.Build(c.rctx.Sctx)
	if vec.Len() == 0 {
		return nil, nil
	}
	return vec, nil
}

// convert casts the values of vec, whose types all have targets, to their
// targets and writes the results to b.
func (c *converter) convert(b *vector.DynamicValueBuilder, vec vector.Any) {
	sctx := c.rctx.Sctx
	out := vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		vec := vecs[0]
		if to := c.target[vec.Type()]; to != nil {
			return cast.Cast(sctx, vec, to)
		}
		return vec
	}, vec)
	var ob scode.Builder
	for i := range out.Len() {
		val := vector.ValueAt(&ob, out, i)
		// A cast that failed (in whole or in part) yields an error.
		if typ := val.Type(); cast.HasError(typ) {
			in := vector.ValueAt(&c.sb, vec, i)
			if to := c.target[in.Type()]; to != nil && typ != to {
				val = sctx.WrapError("inference cast failed (try larger sample size)", in)
			}
		}
		b.Write(val)
	}
}

func (c *converter) enq(val super.Value) bool {
	typ := val.Type()
	q, ok := c.queues[typ]
	if !ok {
		if newInferer(typ) == nil {
			// No string fields... skip
			c.target[typ] = nil
			return true
		}
		c.queues[typ] = make([]super.Value, 0, c.limit)
	}
	c.queues[typ] = append(q, val.Copy())
	return false
}

func (c *converter) infer(q []super.Value) {
	typ := q[0].Type()
	infer := newInferer(typ)
	vals := q
	if c.limit != 0 && len(vals) >= c.limit {
		vals = vals[:c.limit]
	}
	for _, val := range vals {
		infer.load(typ, val.Bytes())
	}
	c.target[typ] = infer.typeof(c.rctx.Sctx, typ)
}
