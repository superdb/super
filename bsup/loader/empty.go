package loader

import (
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type empty struct {
	typeID uint32
}

func (*empty) length() uint32 {
	return 0
}

func newEmpty(meta *bsup.Empty) *empty {
	return &empty{meta.TypeID}
}

func (e *empty) unmarshal(bctx *bsup.Context, projection field.Projection) {
}

func (e *empty) project(loader *FrameLoader, projection field.Projection) vector.Any {
	return vector.NewEmpty(loader.lookupType(e.typeID))
}
