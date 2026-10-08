package runtime

import (
	"github.com/superdb/super"
	"github.com/superdb/super/bsup/loader"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Projection struct {
	sctx       *super.Context
	loader     *loader.FrameLoader
	projection field.Projection
}

func NewProjection(sctx *super.Context, frame *loader.FrameLoader, paths []field.Path) sbuf.Puller {
	return sbuf.NewMaterializer(&Projection{
		sctx:       sctx,
		loader:     frame,
		projection: field.NewProjection(paths),
	})
}

func NewVectorProjection(sctx *super.Context, frame *loader.FrameLoader, paths []field.Path) vio.Puller {
	return &Projection{
		sctx:       sctx,
		loader:     frame,
		projection: field.NewProjection(paths),
	}
}

func (p *Projection) Pull(bool) (vector.Any, error) {
	if loader := p.loader; loader != nil {
		p.loader = nil
		return loader.Load(p.sctx, p.projection)
	}
	return nil, nil
}
