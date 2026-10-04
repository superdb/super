package queryio

import (
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/bsupio"
)

type BSUPWriter struct {
	*bsup.ColumnWriter
	marshaler *super.Marshaler
}

func NewBSUPWriter(w io.Writer) *BSUPWriter {
	m := super.NewMarshaler(super.NewContext())
	m.Decorate(super.StyleSimple)
	return &BSUPWriter{
		ColumnWriter: bsupio.NewColumnWriter(sio.NopCloser(w)),
		marshaler:    m,
	}
}

func (w *BSUPWriter) WriteControl(v any) error {
	val, err := w.marshaler.Marshal(v)
	if err != nil {
		return err
	}
	return w.ColumnWriter.WriteControl(val)
}
