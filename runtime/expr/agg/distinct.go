package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
	"github.com/superdb/super/vector/vhash"
)

// distinct feeds the distinct values of each group to fun.  The distinct
// (group, value) pairs are kept in a hash table per value type.
type distinct struct {
	fun    expr.AggFunc
	n      uint32
	tables []*vhash.Table
	index  map[super.Type]*vhash.Table
}

func newDistinct(f expr.AggFunc) expr.AggFunc {
	return &distinct{fun: f, index: map[super.Type]*vhash.Table{}}
}

func (d *distinct) Resize(n uint32) {
	d.n = n
}

func (d *distinct) Consume(groups []uint32, vec vector.Any) {
	groups = orZeros(groups, vec.Len())
	if dynamic, ok := vec.(*vector.Dynamic); ok {
		for tag, slots := range dynamic.ReverseTagMap() {
			if len(slots) > 0 {
				d.insert(pick(groups, slots), dynamic.Values[tag])
			}
		}
		return
	}
	d.insert(groups, vec)
}

func (d *distinct) insert(groups []uint32, vec vector.Any) {
	if vec.Len() == 0 {
		return
	}
	typ := vec.Type()
	table, ok := d.index[typ]
	if !ok {
		table = vhash.NewTable([]super.Type{super.TypeUint32, typ})
		d.index[typ] = table
		d.tables = append(d.tables, table)
	}
	table.FindOrInsert([]vector.Any{groupsVector(groups), vec})
}

func groupsVector(groups []uint32) vector.Any {
	vals := make([]uint64, len(groups))
	for i, g := range groups {
		vals[i] = uint64(g)
	}
	return vector.NewUint(super.TypeUint32, vals)
}

// each calls fn with the groups and values of each table's distinct pairs.
func (d *distinct) each(sctx *super.Context, fn func(groups []uint32, vals vector.Any)) {
	for _, table := range d.tables {
		keys := table.Keys(sctx)
		gvals := keys[0].(*vector.Uint).Values
		groups := make([]uint32, len(gvals))
		for i, g := range gvals {
			groups[i] = uint32(g)
		}
		fn(groups, keys[1])
	}
}

func (d *distinct) ConsumeAsPartial(groups []uint32, partial vector.Any) {
	if partial.Kind() == vector.KindNull {
		return
	}
	elemGroups, values := expandContainer(orZeros(groups, partial.Len()), partial)
	d.Consume(elemGroups, vector.Deunion(values))
}

func (d *distinct) Result(sctx *super.Context) vector.Any {
	d.fun.Resize(d.n)
	d.each(sctx, d.fun.Consume)
	return d.fun.Result(sctx)
}

func (d *distinct) ResultAsPartial(sctx *super.Context) vector.Any {
	var groups []uint32
	b := vbuild.NewDynamicBuilder()
	d.each(sctx, func(g []uint32, vals vector.Any) {
		groups = append(groups, g...)
		b.Write(vals)
	})
	return groupArrays(sctx, d.n, groups, b.Build(), true)
}
