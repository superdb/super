package bsup

import (
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type TypeValueEncoder struct {
	encoder *Uint32Encoder
	count   uint32
}

func NewTypeValueEncoder(typedefs *TypeDefs, vec *vector.TypeValue) *TypeValueEncoder {
	defs, ids := vec.TypeIDs()
	merger := super.NewTypeDefsMerger(typedefs.Encoder().TypeDefsBuilder, defs)
	mapped := make([]uint32, 0, len(ids))
	for _, localID := range ids {
		mapped = append(mapped, merger.LookupID(localID))
	}
	return &TypeValueEncoder{encoder: NewUint32Encoder(mapped), count: uint32(len(mapped))}
}

func (t *TypeValueEncoder) Emit(w io.Writer) error {
	return t.encoder.Emit(w)
}

func (t *TypeValueEncoder) Encode(group *errgroup.Group) {
	t.encoder.Encode(group)
}

func (t *TypeValueEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.WriteCount(t.count)
	t.locateIDs(w)
}

// locateIDs encodes just the segment of the type IDs, which is how the
// subtypes of a fusion vector are located.
func (t *TypeValueEncoder) locateIDs(w *LocatorWriter) {
	_, loc := t.encoder.Segment(w.Offset())
	w.Segment(loc)
}

func (*TypeValueEncoder) Stats(field.Path, CollectStatsFunc) {}
