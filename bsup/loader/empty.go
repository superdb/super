package loader

import (
	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type empty struct {
	typ super.Type
}

func (*empty) length() uint32 {
	return 0
}

func newEmpty(meta *bsup.Empty) *empty {
	return &empty{typ: meta.Type}
}

func (e *empty) unmarshal(cctx *bsup.Context, projection field.Projection) {
}

func (e *empty) project(loader *loader, projection field.Projection) vector.Any {
	typ, err := loader.sctx.TranslateType(e.typ)
	if err != nil {
		panic(err)
	}
	return vector.NewEmpty(typ)
}
