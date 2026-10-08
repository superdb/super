package loader

import (
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type named struct {
	meta   *bsup.Named
	values shadow
}

func (n *named) length() uint32 {
	return n.values.length()
}

func newNamed(meta *bsup.Named, values shadow) *named {
	return &named{
		meta:   meta,
		values: values,
	}
}

func (n *named) unmarshal(cctx *bsup.Context, projection field.Projection) {
	n.values.unmarshal(cctx, projection)
}

func (n *named) project(loader *loader, projection field.Projection) vector.Any {
	vec := n.values.project(loader, projection)
	// Try to preserve the named type if possible but if the projection changes
	// the underlying type, then just return the inner vector.
	inner := vec.Type()
	named := loader.sctx.LookupByName(n.meta.Name)
	if named == nil {
		var err error
		named, err = loader.sctx.LookupTypeNamed(n.meta.Name, vec.Type())
		if err != nil {
			panic(err)
		}
	} else if named.Type != inner {
		return vec
	}
	return vector.NewNamed(named, vec)
}
