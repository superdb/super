package loader

import (
	"sync"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type map_ struct {
	mu     sync.Mutex
	meta   *bsup.Map
	len    uint32
	offs   []uint32
	keys   shadow
	values shadow
}

func newMap(cctx *bsup.Context, meta *bsup.Map) *map_ {
	return &map_{meta: meta, len: meta.Len(cctx)}
}

func (m *map_) length() uint32 {
	return m.len
}

func (m *map_) unmarshal(cctx *bsup.Context, projection field.Projection) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.keys == nil {
		m.keys = newShadow(cctx, m.meta.Keys)
		m.values = newShadow(cctx, m.meta.Values)
	}
	m.keys.unmarshal(cctx, projection)
	m.values.unmarshal(cctx, projection)
}

func (m *map_) project(loader *FrameLoader, projection field.Projection) vector.Any {
	keys := m.keys.project(loader, nil)
	vals := m.values.project(loader, nil)
	typ := loader.sctx.LookupTypeMap(keys.Type(), vals.Type())
	offs := m.load(loader)
	if len(offs) == 0 {
		offs = []uint32{0}
	}
	return vector.NewMap(typ, offs, keys, vals)
}

func (m *map_) load(loader *FrameLoader) []uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.offs != nil {
		return m.offs
	}
	offs, err := bsup.ReadUint32s(m.meta.Lengths, loader.frame)
	if err != nil {
		panic(err)
	}
	m.offs = offs
	return offs
}
