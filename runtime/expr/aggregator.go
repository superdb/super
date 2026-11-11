package expr

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

// AggFunc computes an aggregation over a set of groups, holding the state
// of every group.  Groups are identified by dense IDs starting at zero.
type AggFunc interface {
	// Resize grows the number of groups to n.  New groups start in the
	// initial state.
	Resize(n uint32)
	// Consume folds each slot of vec into the group given by the
	// corresponding element of groups.  groups is valid only for the
	// duration of the call.
	Consume(groups []uint32, vec vector.Any)
	// ConsumeAsPartial is like Consume but for partial results as
	// produced by ResultAsPartial.
	ConsumeAsPartial(groups []uint32, vec vector.Any)
	// Result returns a vector with the result of each group, in group
	// order.
	Result(*super.Context) vector.Any
	// ResultAsPartial is like Result but returns partial results.
	ResultAsPartial(*super.Context) vector.Any
}

type AggPattern func() AggFunc

type Aggregator struct {
	Pattern  AggPattern
	Name     string
	Distinct bool
	Expr     Evaluator
	Where    Evaluator
	NoRip    bool
}

func NewAggregator(name string, distinct bool, expr Evaluator, where Evaluator, pattern AggPattern) (*Aggregator, error) {
	var norip bool
	if fn, ok := pattern().(interface{ NoRip() bool }); ok {
		norip = fn.NoRip()
	}
	if expr == nil {
		// Count is the only that has no argument so we just return
		// true so it counts each value encountered.
		expr = NewLiteral(nil, super.True)
	}
	return &Aggregator{
		Pattern:  pattern,
		Name:     name,
		Distinct: distinct,
		Expr:     expr,
		Where:    where,
		NoRip:    norip,
	}, nil
}

func (a *Aggregator) Eval(this vector.Any) vector.Any {
	vec := a.Expr.Eval(this)
	if a.Where == nil {
		if a.NoRip {
			vec = &vector.NoRip{Any: vec}
		}
		return vec
	}
	// XXX When there are errors in the filter clause, they should be propagated.
	// We will address this in a subsequent PR.
	where := a.Where.Eval(this)
	bools, _ := BoolMask(where)
	if bools.IsEmpty() {
		// everything is filtered.
		return vector.NewNone(vec.Len())
	}
	if bools.GetCardinality() != uint64(vec.Len()) {
		index := bools.ToArray()
		none := vector.NewNone(vec.Len() - uint32(len(index)))
		vec = vector.Combine(none, index, vector.Pick(vec, index))
	}
	if a.NoRip {
		vec = &vector.NoRip{Any: vec}
	}
	return vec
}

type noRipEval struct {
	eval Evaluator
}

func NoRipEval(e Evaluator) Evaluator {
	return &noRipEval{e}
}

func (n *noRipEval) Eval(vec vector.Any) vector.Any {
	return vector.AddNoRip(n.eval.Eval(vec))
}
