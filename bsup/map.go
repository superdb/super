package bsup

import (
	"io"

	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type MapEncoder struct {
	keys    Encoder
	values  Encoder
	offsets *Uint32Encoder
	count   uint32
}

func NewMapEncoder(typedefs *TypeDefs, vec *vector.Map) *MapEncoder {
	return &MapEncoder{
		keys:    NewEncoder(typedefs, vec.Keys),
		values:  NewEncoder(typedefs, vec.Values),
		offsets: NewUint32Encoder(vec.Offsets),
		count:   vec.Len(),
	}
}

func (m *MapEncoder) Emit(w io.Writer) error {
	if err := m.offsets.Emit(w); err != nil {
		return err
	}
	if err := m.keys.Emit(w); err != nil {
		return err
	}
	return m.values.Emit(w)
}

func (m *MapEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.WriteCount(m.count)
	_, offsets := m.offsets.Segment(w.Offset())
	w.Segment(offsets)
	w.Locate(m.keys)
	w.Locate(m.values)
}

func (*MapEncoder) Stats(field.Path, CollectStatsFunc) {} // Map stats are ignored for now.

func (m *MapEncoder) Encode(group *errgroup.Group) {
	m.offsets.Encode(group)
	m.keys.Encode(group)
	m.values.Encode(group)
}
