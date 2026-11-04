package op

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Values struct {
	sctx   *super.Context
	parent vio.Puller
	exprs  []expr.Evaluator
}

var _ vio.Puller = (*Values)(nil)

// NewValues returns a Values that outputs the values of exprs.  Plain none
// values are dropped (see dropPlainNones).
func NewValues(sctx *super.Context, parent vio.Puller, exprs []expr.Evaluator) *Values {
	return &Values{
		sctx:   sctx,
		parent: parent,
		exprs:  exprs,
	}
}

func (v *Values) Pull(done bool) (vector.Any, error) {
	for {
		val, err := v.parent.Pull(done)
		if val == nil {
			return nil, err
		}
		vals := make([]vector.Any, 0, len(v.exprs))
		for _, e := range v.exprs {
			vals = append(vals, e.Eval(val))
		}
		var out vector.Any
		switch len(vals) {
		case 0:
			continue
		case 1:
			out = vals[0]
		default:
			out = interleave(vals)
		}
		if out = dropPlainNones(out); out != nil {
			return out, nil
		}
	}
}

// XXX should work for vector.Dynamic
func interleave(vals []vector.Any) vector.Any {
	if len(vals) < 2 {
		panic("interleave requires two or more vals")
	}
	n := vals[0].Len()
	nvals := uint32(len(vals))
	tags := make([]uint32, n*nvals)
	for k := uint32(0); k < n*nvals; k++ {
		tags[k] = k % nvals

	}
	return vector.NewDynamic(tags, vals)
}
