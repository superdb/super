package loader

import (
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type none struct {
	meta *bsup.None
}

func newNone(meta *bsup.None) *none {
	return &none{meta: meta}
}

func (n *none) length() uint32 {
	return n.meta.Count
}

func (*none) unmarshal(*bsup.Context, field.Projection) {}

func (n *none) project(loader *FrameLoader, projection field.Projection) vector.Any {
	return vector.NewMissing(n.meta.Count)
}
