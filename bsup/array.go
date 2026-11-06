package bsup

import (
	"io"

	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type ArrayEncoder struct {
	values  Encoder
	offsets *Uint32Encoder
	count   uint32
}

var _ Encoder = (*ArrayEncoder)(nil)

func NewArrayEncoder(typedefs *TypeDefs, vec *vector.Array) *ArrayEncoder {
	return &ArrayEncoder{
		values:  NewEncoder(typedefs, vec.Values),
		offsets: NewUint32Encoder(vec.Offsets),
		count:   vec.Len(),
	}
}

func (a *ArrayEncoder) Encode(group *errgroup.Group) {
	a.offsets.Encode(group)
	a.values.Encode(group)
}

func (a *ArrayEncoder) Emit(w io.Writer) error {
	if err := a.offsets.Emit(w); err != nil {
		return err
	}
	return a.values.Emit(w)
}

func (a *ArrayEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.WriteCount(a.count)
	_, offsets := a.offsets.Segment(w.Offset())
	w.Segment(offsets)
	w.Locate(a.values)
}

func (a *ArrayEncoder) Stats(field.Path, CollectStatsFunc) {} // Array/Set stats are ignored for now.

type SetEncoder struct {
	ArrayEncoder
}

func NewSetEncoder(typedefs *TypeDefs, vec *vector.Set) *SetEncoder {
	return &SetEncoder{
		ArrayEncoder{
			values:  NewEncoder(typedefs, vec.Values),
			offsets: &Uint32Encoder{vals: vec.Offsets},
			count:   vec.Len(),
		},
	}
}
