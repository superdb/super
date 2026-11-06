package bsup

import (
	"io"

	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type FusionEncoder struct {
	values   Encoder
	subtypes *TypeValueEncoder
}

var _ Encoder = (*FusionEncoder)(nil)

func NewFusionEncoder(typedefs *TypeDefs, vec *vector.Fusion) *FusionEncoder {
	return &FusionEncoder{
		values: NewEncoder(typedefs, vec.Values),
		// Call NewTypeValueEncoder directly because we do not want subtypes
		// wrapped in a dict / const.
		subtypes: NewTypeValueEncoder(typedefs, vec.Subtypes),
	}
}

func (f *FusionEncoder) Emit(w io.Writer) error {
	if err := f.values.Emit(w); err != nil {
		return err
	}
	return f.subtypes.Emit(w)
}

func (f *FusionEncoder) Encode(group *errgroup.Group) {
	f.values.Encode(group)
	f.subtypes.Encode(group)
}

func (f *FusionEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.Locate(f.values)
	f.subtypes.locateIDs(w)
}

func (i *FusionEncoder) Stats(path field.Path, fn CollectStatsFunc) {
	i.values.Stats(path, fn)
}
