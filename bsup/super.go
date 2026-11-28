package bsup

import (
	"io"

	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type SuperEncoder struct {
	values   Encoder
	subtypes *TypeValueEncoder
}

var _ Encoder = (*SuperEncoder)(nil)

func NewSuperEncoder(typedefs *TypeDefs, vec *vector.Super) *SuperEncoder {
	return &SuperEncoder{
		values: NewEncoder(typedefs, vec.Values),
		// Call NewTypeValueEncoder directly because we do not want subtypes
		// wrapped in a dict / const.
		subtypes: NewTypeValueEncoder(typedefs, vec.Subtypes),
	}
}

func (f *SuperEncoder) Emit(w io.Writer) error {
	if err := f.values.Emit(w); err != nil {
		return err
	}
	return f.subtypes.Emit(w)
}

func (f *SuperEncoder) Encode(group *errgroup.Group) {
	f.values.Encode(group)
	f.subtypes.Encode(group)
}

func (f *SuperEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.Locate(f.values)
	f.subtypes.locateIDs(w)
}

func (i *SuperEncoder) Stats(path field.Path, fn CollectStatsFunc) {
	i.values.Stats(path, fn)
}
