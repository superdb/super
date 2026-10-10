package loader

import (
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type _any struct {
	mu       sync.Mutex
	cctx     *bsup.Context
	meta     *bsup.Any
	len      uint32
	values   shadow
	subtypes *typevalue
}

func newAny(cctx *bsup.Context, meta *bsup.Any) *_any {
	return &_any{
		cctx: cctx,
		meta: meta,
		len:  meta.Len(cctx),
	}
}

func (f *_any) length() uint32 {
	return f.len
}

func (f *_any) unmarshal(cctx *bsup.Context, projection field.Projection) {
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

func (f *_any) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vec := f.values.project(loader, projection)
	typ := loader.sctx.LookupTypeFusion(super.TypeAll)
	return vector.NewFusionWithLoader(loader.sctx, typ, f.subtypes.newLoader(loader), vec)
}
