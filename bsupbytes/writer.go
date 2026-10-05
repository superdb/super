package bsupbytes

import (
	"bytes"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/sio"
)

type Writer struct {
	marshaler *super.Marshaler
	buffer    bytes.Buffer
	writer    *bsup.RowWriter
}

func NewWriter() *Writer {
	m := super.NewMarshaler(super.NewContext())
	b := &Writer{
		marshaler: m,
	}
	b.writer = bsup.NewRowWriter(sio.NopCloser(&b.buffer))
	return b
}

func NewWriterWithStyle(style super.TypeStyle) *Writer {
	b := NewWriter()
	b.marshaler.Decorate(style)
	return b
}

func (w *Writer) Write(v any) error {
	val, err := w.marshaler.Marshal(v)
	if err != nil {
		return err
	}
	return w.writer.Write(val)
}

// Bytes returns a slice holding the serialized values.  Close must be called
// before Bytes.
func (w *Writer) Bytes() []byte {
	return w.buffer.Bytes()
}

func (w *Writer) Close() error {
	return w.writer.Close()
}
