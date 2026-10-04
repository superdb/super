package bsupbytes

import (
	"bytes"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/sio"
)

type BytesWriter struct {
	marshaler *super.Marshaler
	buffer    bytes.Buffer
	writer    *bsup.RowWriter
}

func NewBytesWriter() *BytesWriter {
	m := super.NewMarshaler(super.NewContext())
	b := &BytesWriter{
		marshaler: m,
	}
	b.writer = bsup.NewRowWriter(sio.NopCloser(&b.buffer))
	return b
}

func NewBytesWriterWithStyle(style super.TypeStyle) *BytesWriter {
	b := NewBytesWriter()
	b.marshaler.Decorate(style)
	return b
}

func (b *BytesWriter) Write(v any) error {
	val, err := b.marshaler.Marshal(v)
	if err != nil {
		return err
	}
	return b.writer.Write(val)
}

// Bytes returns a slice holding the serialized values.  Close must be called
// before Bytes.
func (b *BytesWriter) Bytes() []byte {
	return b.buffer.Bytes()
}

func (b *BytesWriter) Close() error {
	return b.writer.Close()
}
