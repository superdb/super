package aggregate

import (
	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Aggregate struct {
	parent vio.Puller
	sctx   *super.Context
	defuse *expr.SuperDefuse
	// XX Abstract this runtime into a generic table computation.
	// Then the generic interface can execute fast paths for simple scenarios.
	aggs        []*expr.Aggregator
	aggExprs    []expr.Evaluator
	keyExprs    []expr.Evaluator
	typeTable   *super.TypeVectorTable
	builder     *vector.RecordBuilder
	partialsIn  bool
	partialsOut bool

	types   []super.Type
	tables  map[int]aggTable
	results []aggTable
}

func New(parent vio.Puller, sctx *super.Context, aggNames []field.Path, aggExprs []expr.Evaluator, aggs []*expr.Aggregator, keyNames []field.Path, keyExprs []expr.Evaluator, partialsIn, partialsOut bool) (*Aggregate, error) {
	builder, err := vector.NewRecordBuilder(sctx, append(keyNames, aggNames...))
	if err != nil {
		return nil, err
	}
	return &Aggregate{
		parent:      parent,
		sctx:        sctx,
		defuse:      expr.NewSuperDefuse(sctx),
		aggs:        aggs,
		aggExprs:    aggExprs,
		keyExprs:    keyExprs,
		tables:      make(map[int]aggTable),
		typeTable:   super.NewTypeVectorTable(),
		types:       make([]super.Type, len(keyExprs)),
		builder:     builder,
		partialsIn:  partialsIn,
		partialsOut: partialsOut,
	}, nil
}

func (a *Aggregate) Pull(done bool) (vector.Any, error) {
	if done {
		_, err := a.parent.Pull(done)
		return nil, err
	}
	if a.results != nil {
		return a.next(), nil
	}
	for {
		//XXX check context Done
		vec, err := a.parent.Pull(false)
		if err != nil {
			return nil, err
		}
		if vec == nil {
			for _, t := range a.tables {
				a.results = append(a.results, t)
			}
			clear(a.tables)
			return a.next(), nil
		}
		var keys, vals []vector.Any
		for _, e := range a.keyExprs {
			keys = append(keys, a.defuse.Eval(e.Eval(vec)))
		}
		if a.partialsIn {
			for _, e := range a.aggExprs {
				vals = append(vals, e.Eval(vec))
			}
		} else {
			for _, e := range a.aggs {
				vals = append(vals, e.Eval(vec))
			}
		}
		vector.Apply(vector.ApplyRipUnions|vector.ApplyRipFusions, func(args ...vector.Any) vector.Any {
			// XXX need to call Catch here on args and keys and push errors out of the table here
			// in addition to the table results at the end.  agg func Consume methods should
			// decide whether they want to process the errors (like collect?) or have the errors
			// pushed (e.g., count, sum, etc)... I think the errors should always be pushed.
			// This will be addressed n a subsequent PR.
			a.consume(args[:len(keys)], args[len(keys):])
			// XXX Perhaps there should be a "consume" version of Apply where
			// no return value is expected.
			return vector.NewNull(args[0].Len())
		}, append(keys, vals...)...)
	}
}

func (a *Aggregate) consume(keys []vector.Any, vals []vector.Any) {
	if keys[0].Len() == 0 {
		return
	}
	var keyTypes []super.Type
	for _, k := range keys {
		keyTypes = append(keyTypes, k.Type())
	}
	tableID := a.typeTable.Lookup(keyTypes)
	table, ok := a.tables[tableID]
	if !ok {
		table = a.newAggTable(keyTypes)
		a.tables[tableID] = table
	}
	table.update(keys, vals)
}

func (a *Aggregate) newAggTable(keyTypes []super.Type) aggTable {
	// Check if we can us an optimized table, else go slow path.
	if a.isCountByString(keyTypes) && len(a.aggs) == 1 && a.aggs[0].Where == nil {
		// countByString.update does not handle nulls in its vals param.
		return newCountByString(keyTypes[0], a.builder, a.partialsIn)
	}
	return &superTable{
		aggs:        a.aggs,
		builder:     a.builder,
		partialsIn:  a.partialsIn,
		partialsOut: a.partialsOut,
		table:       make(map[string]int),
		sctx:        a.sctx,
	}
}

func (a *Aggregate) isCountByString(keyTypes []super.Type) bool {
	return len(a.aggs) == 1 && a.aggs[0].Name == "count" && !a.aggs[0].Distinct &&
		len(keyTypes) == 1 && keyTypes[0].ID() == super.IDString
}

func (a *Aggregate) next() vector.Any {
	if len(a.results) == 0 {
		a.results = nil
		return nil
	}
	t := a.results[0]
	a.results = a.results[1:]
	return t.materialize()
}
