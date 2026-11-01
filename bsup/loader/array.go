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

func newArray(bctx *bsup.Context, meta *bsup.Array) *array {
	return &array{meta: meta, len: meta.Len(bctx)}
}

func (a *array) length() uint32 {
	return a.len
}

func (a *array) unmarshal(bctx *bsup.Context, projection field.Projection) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.values == nil {
		a.values = newShadow(bctx, a.meta.Values)
	}
	a.values.unmarshal(bctx, projection)
}

func (a *array) project(loader *FrameLoader, projection field.Projection) vector.Any {
	if projection.TypesOnly() {
		// The type of an array doesn't depend on its offsets but if the
		// elements contain a fusion value, then the data is needed to
		// compute the type of the superdefused value.
		vec := a.values.project(loader, projection)
		if !hasFusion(vec.Type()) {
			typ := loader.sctx.LookupTypeArray(vec.Type())
			return vector.NewTypeOnly(typ, listLen(a.load(loader)))
		}
	}
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
	offs, err := bsup.ReadUint32s(a.meta.Lengths, loader.frame)
	if err != nil {
		panic(err)
	}
	a.offs = offs
	return offs
}

// listLen returns the length of a list vector with offsets offs.  (The length
// in the BSUP metadata for arrays, sets, and maps can't be used as it isn't
// always set by the encoder.)
func listLen(offs []uint32) uint32 {
	if len(offs) == 0 {
		return 0
	}
	return uint32(len(offs) - 1)
}
