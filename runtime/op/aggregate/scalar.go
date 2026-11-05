package aggregate

import (
	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type scalarAggregate struct {
	parent      vio.Puller
	sctx        *super.Context
	aggExprs    []expr.Evaluator
	aggs        []*expr.Aggregator
	builder     *vector.RecordBuilder
	partialsIn  bool
	partialsOut bool

	funcs []expr.AggFunc
}

func NewScalar(parent vio.Puller, sctx *super.Context, aggs []*expr.Aggregator, aggNames []field.Path, aggExprs []expr.Evaluator, partialsIn, partialsOut bool) (vio.Puller, error) {
	builder, err := vector.NewRecordBuilder(sctx, aggNames)
	if err != nil {
		return nil, err
	}
	return &scalarAggregate{
		parent:      parent,
		sctx:        sctx,
		aggs:        aggs,
		aggExprs:    aggExprs,
		builder:     builder,
		partialsIn:  partialsIn,
		partialsOut: partialsOut,
		funcs:       newFuncs(aggs),
	}, nil
}

func (s *scalarAggregate) Pull(done bool) (vector.Any, error) {
	if s.funcs == nil {
		s.funcs = newFuncs(s.aggs)
		return nil, nil
	}
	for {
		vec, err := s.parent.Pull(done)
		if err != nil {
			return nil, err
		}
		if vec == nil {
			return s.result(), nil
		}
		var vals []vector.Any
		if s.partialsIn {
			for _, e := range s.aggExprs {
				vals = append(vals, e.Eval(vec))
			}
		} else {
			for _, e := range s.aggs {
				vals = append(vals, e.Eval(vec))
			}
		}
		vector.Apply(vector.ApplyRipUnions|vector.ApplyRipFusions, s.consume, vals...)
	}
}

func (s *scalarAggregate) consume(vecs ...vector.Any) vector.Any {
	for i, vec := range vecs {
		if s.partialsIn {
			s.funcs[i].ConsumeAsPartial(nil, vec)
		} else {
			s.funcs[i].Consume(nil, vec)
		}
	}
	return vector.NewNull(vecs[0].Len())
}

func newFuncs(aggs []*expr.Aggregator) []expr.AggFunc {
	var funcs []expr.AggFunc
	for _, agg := range aggs {
		f := agg.Pattern()
		f.Resize(1)
		funcs = append(funcs, f)
	}
	return funcs
}

func (s *scalarAggregate) result() vector.Any {
	var vecs []vector.Any
	for _, f := range s.funcs {
		var vec vector.Any
		if s.partialsOut {
			vec = f.ResultAsPartial(s.sctx)
		} else {
			vec = f.Result(s.sctx)
		}
		vecs = append(vecs, vec)
	}
	s.funcs = nil
	return s.builder.New(vecs)
}
