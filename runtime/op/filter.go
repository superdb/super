package op

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Filter struct {
	sctx    *super.Context
	parent  vio.Puller
	expr    expr.Evaluator
	catch   bool
	pending vector.Any
}

// NewFilter returns a filter that outputs the values of parent for which expr
// is true.  If catch is true, the filter also outputs a SideErrors for the
// values on which expr produces an error (see NewSideErrors).  Otherwise, those
// values are dropped.
func NewFilter(sctx *super.Context, parent vio.Puller, expr expr.Evaluator, catch bool) *Filter {
	return &Filter{sctx: sctx, parent: parent, expr: expr, catch: catch}
}

func (f *Filter) Pull(done bool) (vector.Any, error) {
	if done {
		f.pending = nil
		return f.parent.Pull(true)
	}
	for {
		if f.pending != nil {
			vec := f.pending
			f.pending = nil
			return vec, nil
		}
		vec, err := f.parent.Pull(false)
		if vec == nil || err != nil {
			return nil, err
		}
		mask := f.expr.Eval(vec)
		if f.catch {
			if _, errs := expr.BoolMask(mask); !errs.IsEmpty() {
				slots := errs.ToArray()
				f.pending = NewSideErrors(f.sctx, "where: predicate error", vector.Pick(vec, slots), vector.Pick(mask, slots))
			}
		}
		if masked, ok := applyMask(vec, mask); ok {
			return masked, nil
		}
	}
}

// applyMask applies the mask vector mask to vec.  Elements of mask that are not
// Boolean are considered false.
func applyMask(vec, mask vector.Any) (vector.Any, bool) {
	// errors are ignored for filters
	b, _ := expr.BoolMask(mask)
	if b.IsEmpty() {
		return nil, false
	}
	if b.GetCardinality() == uint64(mask.Len()) {
		return vec, true
	}
	return vector.Pick(vec, b.ToArray()), true
}
