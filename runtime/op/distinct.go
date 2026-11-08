package op

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vhash"
	"github.com/superdb/super/vector/vio"
)

type Distinct struct {
	sctx   *super.Context
	parent vio.Puller
	expr   expr.Evaluator
	defuse *expr.SuperDefuse
	tables map[super.Type]*vhash.Table
	// scratch
	index []uint32
}

func NewDistinct(sctx *super.Context, parent vio.Puller, e expr.Evaluator) *Distinct {
	return &Distinct{
		sctx:   sctx,
		parent: parent,
		expr:   e,
		defuse: expr.NewSuperDefuse(sctx),
		tables: map[super.Type]*vhash.Table{},
	}
}

func (d *Distinct) Pull(done bool) (vector.Any, error) {
	for {
		vec, err := d.parent.Pull(done)
		if vec == nil || err != nil {
			clear(d.tables)
			return nil, err
		}
		keyVec := d.defuse.Eval(d.expr.Eval(vec))
		d.index = d.index[:0]
		d.insert(keyVec, nil)
		if len(d.index) > 0 {
			// vhash reports new keys in creation order, not row order
			// (salt collisions defer rows to a later pass), and Dynamic
			// keys are inserted per type.
			// XXX There are use cases (e.g., distinct before an aggregation)
			// where we don't care about maintaining order so we should have a
			// way to skip this when not needed.
			if !slices.IsSorted(d.index) {
				slices.Sort(d.index)
			}
			return vector.Pick(vec, slices.Clone(d.index)), nil
		}
	}
}

func (d *Distinct) insert(vec vector.Any, rows []uint32) {
	if dynamic, ok := vec.(*vector.Dynamic); ok {
		for tag, slots := range dynamic.ReverseTagMap() {
			if len(slots) == 0 {
				continue
			}
			d.insert(dynamic.Values[tag], slots)
		}
		return
	}
	if vec.Len() == 0 {
		return
	}
	typ := vec.Type()
	table, ok := d.tables[typ]
	if !ok {
		table = vhash.NewTable([]super.Type{typ})
		d.tables[typ] = table
	}
	_, created := table.FindOrInsert([]vector.Any{vec})
	for _, slot := range created {
		if rows != nil {
			slot = rows[slot]
		}
		d.index = append(d.index, slot)
	}
}
