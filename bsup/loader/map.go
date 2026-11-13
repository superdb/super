package loader

import (
	"sync"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

// Map is a map vector, whose locators are its kind, count, the segment of its
// offsets, and the locators of its keys and values.
type Map struct {
	Node
	Offsets bsup.Segment
	Keys    Shadow
	Values  Shadow
	mu      sync.Mutex
	offs    []uint32
}

func (m *Map) project(loader *FrameLoader, projection field.Projection) vector.Any {
	if projection.TypesOnly() {
		// See Array.project.
		if typ := loader.vectorType(m.TypeID); !hasFusion(typ) {
			return vector.NewTypeOnly(typ, m.Length)
		}
	}
	keys := m.Keys.project(loader, nil)
	vals := m.Values.project(loader, nil)
	typ := loader.sctx.LookupTypeMap(keys.Type(), vals.Type())
	return vector.NewMap(typ, m.load(loader), keys, vals)
}

func (m *Map) load(loader *FrameLoader) []uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.offs == nil {
		m.offs = loadOffsets(loader, m.Offsets, m.Length, m.Keys.Len())
	}
	return m.offs
}
