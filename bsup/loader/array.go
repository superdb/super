package loader

import (
	"sync"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type array struct {
	mu     sync.Mutex
	meta   *bsup.Array
	len    uint32
	offs   []uint32
	values shadow
}

func newArray(cctx *bsup.Context, meta *bsup.Array) *array {
	return &array{meta: meta, len: meta.Len(cctx)}
}

func (a *array) length() uint32 {
	return a.len
}

func (a *array) unmarshal(cctx *bsup.Context, projection field.Projection) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.values == nil {
		a.values = newShadow(cctx, a.meta.Values)
	}
	a.values.unmarshal(cctx, projection)
}

func (a *array) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vec := a.values.project(loader, nil)
	typ := loader.sctx.LookupTypeArray(vec.Type())
	offs := a.load(loader)
	if len(offs) == 0 {
		offs = []uint32{0}
	}
	return vector.NewArray(typ, offs, vec)
}

func (a *array) load(loader *FrameLoader) []uint32 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.offs != nil {
		return a.offs
	}
	offs, err := bsup.ReadUint32s(a.meta.Lengths, loader.frame.DataReader())
	if err != nil {
		panic(err)
	}
	a.offs = offs
	return offs
}
