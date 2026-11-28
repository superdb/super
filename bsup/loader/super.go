package loader

import (
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

// Super is a super vector, whose locators are its kind, the locators of its
// values, and the segment of its subtypes.
type Super struct {
	Node
	Values   Shadow
	Subtypes *TypeValue
}

func (f *Super) project(loader *FrameLoader, projection field.Projection) vector.Any {
	if len(projection) != 0 && !projection.TypesOnly() {
		if !projection.HasTypes() {
			return project(loader, f.Values, projection)
		}
		// The type of the super value is needed along with some of
		// its data so load all of it.
		projection = nil
	}
	// If only the type is needed, the values are projected for their
	// types and the subtypes are loaded lazily.
	vec := project(loader, f.Values, projection)
	typ := loader.sctx.MustLookupTypeSuper(vec.Type())
	return vector.NewSuperWithLoader(loader.sctx, typ, f.Subtypes.newLoader(loader), vec)
}

// SuperAll is a super(all) vector, whose values are bytes.
type SuperAll struct {
	Super
}

func (a *SuperAll) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vec := project(loader, a.Values, projection)
	return vector.NewSuperWithLoader(loader.sctx, loader.sctx.SuperAll(), a.Subtypes.newLoader(loader), vec)
}
