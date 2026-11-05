package loader

import (
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

// Named is a vector of a named type, whose locators are its kind and the
// locators of the vector of its inner type.
type Named struct {
	Node
	Name   string
	Values Shadow
}

func (n *Named) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vec := n.Values.project(loader, projection)
	// Try to preserve the named type if possible but if the projection changes
	// the underlying type, then just return the inner vector.
	inner := vec.Type()
	named := loader.sctx.LookupByName(n.Name)
	if named == nil {
		var err error
		named, err = loader.sctx.LookupTypeNamed(n.Name, vec.Type())
		if err != nil {
			panic(err)
		}
	} else if named.Type != inner {
		return vec
	}
	return vector.NewNamed(named, vec)
}

// Error is an error vector, whose locators are its kind and the locators of
// its values.
type Error struct {
	Node
	Values Shadow
}

func (e *Error) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vec := e.Values.project(loader, projection)
	typ := loader.sctx.LookupTypeError(vec.Type())
	return vector.NewError(typ, vec)
}

// Enum is an enum vector, whose locators are its kind, count, and the segment
// of its uint64 values.
type Enum struct {
	Node
	Symbols []string
	Values  *Uint
}

func (e *Enum) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vec := e.Values.project(loader, projection).(*vector.Uint)
	return &vector.Enum{Uint: vec, Typ: loader.sctx.LookupTypeEnum(e.Symbols)}
}
