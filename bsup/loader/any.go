package loader

import (
	"sync"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type _any struct {
	mu       sync.Mutex
	bctx     *bsup.Context
	meta     *bsup.Any
	len      uint32
	values   shadow
	subtypes *typevalue
}

func newAny(bctx *bsup.Context, meta *bsup.Any) *_any {
	return &_any{
		bctx: bctx,
		meta: meta,
		len:  meta.Len(bctx),
	}
}

func (f *_any) length() uint32 {
	return f.len
}

func (f *_any) unmarshal(bctx *bsup.Context, projection field.Projection) {
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

func (f *_any) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vec := f.values.project(loader, projection)
	return vector.NewFusionWithLoader(loader.sctx, loader.sctx.TypeAny(), f.subtypes.newLoader(loader), vec)
}
