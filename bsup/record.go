package bsup

import (
	"io"

	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type RecordEncoder struct {
	fields []*FieldEncoder
	count  uint32
}

var _ Encoder = (*RecordEncoder)(nil)

func NewRecordEncoder(typedefs *TypeDefs, vec *vector.Record) *RecordEncoder {
	fields := make([]*FieldEncoder, 0, len(vec.Fields))
	for i, f := range vec.Fields {
		fields = append(fields, &FieldEncoder{
			name:   vec.Typ.Fields[i].Name,
			values: NewEncoder(typedefs, f),
		})
	}
	return &RecordEncoder{fields: fields, count: vec.Len()}
}

func (r *RecordEncoder) Encode(group *errgroup.Group) {
	for _, f := range r.fields {
		f.Encode(group, r.count)
	}
}

func (r *RecordEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	if len(r.fields) == 0 {
		// A record with no fields has no field to get its length from.
		w.WriteCount(r.count)
		return
	}
	for _, field := range r.fields {
		w.Locate(field.values)
	}
}

func (r *RecordEncoder) Stats(path field.Path, fn CollectStatsFunc) {
	for _, field := range r.fields {
		field.Stats(path, fn)
	}
}

func (r *RecordEncoder) Emit(w io.Writer) error {
	for _, f := range r.fields {
		if err := f.Emit(w); err != nil {
			return err
		}
	}
	return nil
}
