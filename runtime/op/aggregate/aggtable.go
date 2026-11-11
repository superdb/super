package aggregate

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vhash"
)

// one aggTable per fixed set of types of aggs and keys.
type aggTable interface {
	update([]vector.Any, []vector.Any)
	materialize() vector.Any
}

type superTable struct {
	builder     *vector.RecordBuilder
	partialsIn  bool
	partialsOut bool
	table       *vhash.Table
	funcs       []expr.AggFunc // one per aggregation, holding every group
	sctx        *super.Context
}

var _ aggTable = (*superTable)(nil)

func newSuperTable(a *Aggregate, keyTypes []super.Type) *superTable {
	var funcs []expr.AggFunc
	for _, agg := range a.aggs {
		funcs = append(funcs, agg.Pattern())
	}
	return &superTable{
		builder:     a.builder,
		partialsIn:  a.partialsIn,
		partialsOut: a.partialsOut,
		table:       vhash.NewTable(keyTypes),
		funcs:       funcs,
		sctx:        a.sctx,
	}
}

func (s *superTable) update(keys []vector.Any, args []vector.Any) {
	keyIDs, created := s.table.FindOrInsert(keys)
	if len(created) > 0 {
		for _, f := range s.funcs {
			f.Resize(s.table.Len())
		}
	}
	for i, arg := range args {
		if s.partialsIn {
			s.funcs[i].ConsumeAsPartial(keyIDs, arg)
		} else {
			s.funcs[i].Consume(keyIDs, arg)
		}
	}
}

func (s *superTable) materialize() vector.Any {
	if s.table.Len() == 0 {
		return vector.NewNull(0)
	}
	vecs := s.table.Keys(s.sctx)
	for _, f := range s.funcs {
		if s.partialsOut {
			vecs = append(vecs, f.ResultAsPartial(s.sctx))
		} else {
			vecs = append(vecs, f.Result(s.sctx))
		}
	}
	// Since aggs can return dynamic values need to do apply to create record.
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		return s.builder.New(vecs)
	}, vecs...)
}
