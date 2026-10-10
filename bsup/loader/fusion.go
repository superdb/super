package loader

import (
	"sync"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type fusion struct {
	mu       sync.Mutex
	cctx     *bsup.Context
	meta     *bsup.Fusion
	len      uint32
	values   shadow
	subtypes *typevalue
}

func newFusion(cctx *bsup.Context, meta *bsup.Fusion) *fusion {
	return &fusion{
		cctx: cctx,
		meta: meta,
		len:  meta.Len(cctx),
	}
}

func (f *fusion) length() uint32 {
	return f.len
}

func (f *fusion) unmarshal(cctx *bsup.Context, projection field.Projection) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.values == nil {
		f.values = newShadow(cctx, f.meta.Values)
	}
	if f.subtypes == nil {
		f.subtypes = newTypeValue(cctx, cctx.Lookup(f.meta.Subtypes).(*bsup.TypeValue))
	}
	f.values.unmarshal(cctx, projection)
}

func (f *fusion) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vec := f.values.project(loader, projection)
	typ := loader.sctx.LookupTypeFusion(vec.Type())
	return vector.NewFusionWithLoader(loader.sctx, typ, f.subtypes.newLoader(loader), vec)
}
