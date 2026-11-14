package loader

import (
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

// Fusion is a fusion vector, whose locators are its kind, the locators of its
// values, and the segment of its subtypes.
type Fusion struct {
	Node
	Values   Shadow
	Subtypes *TypeValue
}

func (f *Fusion) project(loader *FrameLoader, projection field.Projection) vector.Any {
	if len(projection) != 0 && !projection.TypesOnly() {
		if !projection.HasTypes() {
			return f.Values.project(loader, projection)
		}
		// The type of the fusion value is needed along with some of
		// its data so load all of it.
		projection = nil
	}
	// If only the type is needed, the values are projected for their
	// types and the subtypes are loaded lazily.
	vec := f.Values.project(loader, projection)
	typ := loader.sctx.MustLookupTypeFusion(vec.Type())
	return vector.NewFusionWithLoader(loader.sctx, typ, f.Subtypes.newLoader(loader), vec)
}

// FusionAll is a fusion(all) vector, whose values are bytes.
type FusionAll struct {
	Fusion
}

func (a *FusionAll) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vec := a.Values.project(loader, projection)
	return vector.NewFusionWithLoader(loader.sctx, loader.sctx.FusionAll(), a.Subtypes.newLoader(loader), vec)
}
