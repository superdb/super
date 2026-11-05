package bsup

import (
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type TypeValueEncoder struct {
	bctx    *Context
	encoder *Uint32Encoder
}

func NewTypeValueEncoder(bctx *Context, vec *vector.TypeValue) Encoder {
	defs, ids := vec.TypeIDs()
	merger := super.NewTypeDefsMerger(bctx.TypeDefs().TypeDefsBuilder, defs)
	mapped := make([]uint32, 0, len(ids))
	for _, localID := range ids {
		mapped = append(mapped, merger.LookupID(localID))
	}
	return &TypeValueEncoder{bctx: bctx, encoder: NewUint32Encoder(mapped)}
}

func (t *TypeValueEncoder) Emit(w io.Writer) error {
	return t.encoder.Emit(w)
}

func (t *TypeValueEncoder) Encode(group *errgroup.Group) {
	t.encoder.Encode(group)
}

func (t *TypeValueEncoder) Metadata(bctx *Context, off uint64) (uint64, ID) {
	off, loc := t.encoder.Segment(off)
	return off, bctx.enter(&TypeValue{
		Location: loc,
	})
}

func (*TypeValueEncoder) Stats(field.Path, CollectStatsFunc) {}
