package loader

import (
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type null struct {
	meta *bsup.Null
}

func newNull(meta *bsup.Null) *null {
	return &null{meta: meta}
}

func (n *null) length() uint32 {
	return n.meta.Count
}

func (*null) unmarshal(*bsup.Context, field.Projection) {}

func (n *null) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vec := vector.NewNull(n.meta.Count)
	if len(projection) > 0 {
		return vector.NewWrappedError(loader.sctx, "'.': applied to non-record", vec)
	}
	return vec
}
