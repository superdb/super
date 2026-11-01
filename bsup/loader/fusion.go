package loader

import (
	"sync"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type fusion struct {
	mu       sync.Mutex
	bctx     *bsup.Context
	meta     *bsup.Fusion
	len      uint32
	values   shadow
	subtypes *typevalue
}

func newFusion(bctx *bsup.Context, meta *bsup.Fusion) *fusion {
	return &fusion{
		bctx: bctx,
		meta: meta,
		len:  meta.Len(bctx),
	}
}

func (f *fusion) length() uint32 {
	return f.len
}

func (f *fusion) unmarshal(bctx *bsup.Context, projection field.Projection) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.values == nil {
		f.values = newShadow(bctx, f.meta.Values)
	}
	if f.subtypes == nil {
		f.subtypes = newTypeValue(bctx, bctx.Lookup(f.meta.Subtypes).(*bsup.TypeValue))
	}
	f.values.unmarshal(bctx, projection)
}

func (f *fusion) project(loader *FrameLoader, projection field.Projection) vector.Any {
	if len(projection) != 0 && !projection.TypesOnly() {
		if !projection.HasTypes() {
			return f.values.project(loader, projection)
		}
		// The type of the fusion value is needed along with some of
		// its data so load all of it.
		projection = nil
	}
	// If only the type is needed, the values are projected for their
	// types and the subtypes are loaded lazily.
	vec := f.values.project(loader, projection)
	typ := loader.sctx.MustLookupTypeFusion(vec.Type())
	return vector.NewFusionWithLoader(loader.sctx, typ, f.subtypes.newLoader(loader), vec)
}
