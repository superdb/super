package storage

import (
	"bytes"
	"io"
)

// Buffer holds the entire contents of an input in memory so that an input
// that can be read only once (e.g., standard input or an HTTP response) can
// be read from any offset and read again.
type Buffer struct {
	b []byte
}

// ReadBuffer reads r to EOF into a Buffer.
func ReadBuffer(r io.Reader) (*Buffer, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return &Buffer{b}, nil
}

// Reader returns a new Reader positioned at the start of the buffer.  The
// Reader also implements io.Seeker.
func (b *Buffer) Reader() Reader {
	return &bufferReader{bytes.NewReader(b.b)}
}

func (b *Buffer) Len() int {
	return len(b.b)
}

type bufferReader struct {
	*bytes.Reader
}

func (*bufferReader) Close() error { return nil }
