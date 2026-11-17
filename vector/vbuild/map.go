package vbuild

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type mapBuilder struct {
	typ     *super.TypeMap
	keys    Builder
	vals    Builder
	offsets []uint32
	len     uint32
}

func newMapBuilder(typ *super.TypeMap) Builder {
	return &mapBuilder{
		typ:     typ,
		keys:    New(typ.KeyType),
		vals:    New(typ.ValType),
		offsets: []uint32{0},
	}
}

func (m *mapBuilder) Write(vec vector.Any) {
	n := vec.Len()
	if n == 0 {
		return
	}
	vmap := vector.PushViewOrConst(vec).(*vector.Map)
	m.keys.Write(vmap.Keys)
	m.vals.Write(vmap.Values)
	for i := range n {
		m.len += vmap.Offsets[i+1] - vmap.Offsets[i]
		m.offsets = append(m.offsets, m.len)
	}
}

func (m *mapBuilder) Build() vector.Any {
	return vector.NewMap(m.typ, m.offsets, m.keys.Build(), m.vals.Build())
}
