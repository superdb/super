package expr

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type AggFunc interface {
	Consume(vector.Any)
	ConsumeAsPartial(vector.Any)
	Result(*super.Context) vector.Any
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
	where := a.Where.Eval(this)
	bools, _ := BoolMask(where)
	if bools.IsEmpty() {
		// everything is filtered.
		return vector.NewMissing(vec.Len())
	}
	if bools.GetCardinality() != uint64(vec.Len()) {
		index := bools.ToArray()
		// XXX TBD - add type safe error checking here
		missing := vector.NewMissing(vec.Len() - uint32(len(index)))
		vec = vector.Combine(missing, index, vector.Pick(vec, index))
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
