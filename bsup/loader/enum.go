package loader

import (
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type enum struct {
	meta   *bsup.Enum
	values shadow
}

func (e *enum) length() uint32 {
	return e.values.length()
}

func newEnum(meta *bsup.Enum, values shadow) *enum {
	return &enum{
		meta:   meta,
		values: values,
	}
}

func (e *enum) unmarshal(bctx *bsup.Context, projection field.Projection) {
	e.values.unmarshal(bctx, projection)
}

func (e *enum) project(loader *FrameLoader, projection field.Projection) vector.Any {
	enum := loader.sctx.LookupTypeEnum(e.meta.Symbols)
	if projection.TypesOnly() {
		return vector.NewTypeOnly(enum, e.length())
	}
	vec := e.values.project(loader, projection).(*vector.Uint)
	return &vector.Enum{Uint: vec, Typ: enum}
}
