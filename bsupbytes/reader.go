package bsupbytes

import (
	"bytes"
	"context"
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/bsupio"
)

type Reader struct {
	reader      sio.Reader
	unmarshaler *super.Unmarshaler
}

func NewReader(reader sio.Reader, templates []any) *Reader {
	u := super.NewUnmarshaler()
	u.Bind(templates...)
	return &Reader{
		reader:      reader,
		unmarshaler: u,
	}
}

func NewReaderFromBytes(ctx context.Context, b []byte, templates []any) (*Reader, error) {
	reader, err := bsupio.NewValueReader(ctx, super.NewContext(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return NewReader(reader, templates), nil
}

func (r *Reader) Read() (any, error) {
	val, err := r.reader.Read()
	if val == nil || err != nil {
		return nil, err
	}
	var action any
	if err := r.unmarshaler.Unmarshal(*val, &action); err != nil {
		return nil, err
	}
	return action, nil
}

type ReadCloser struct {
	*Reader
	io.Closer
}

func Get(ctx context.Context, engine storage.Engine, uri *storage.URI, templates []any) (*ReadCloser, error) {
	r, err := engine.Get(ctx, uri)
	if err != nil {
		return nil, err
	}
	reader, err := bsupio.NewValueReader(ctx, super.NewContext(), r)
	return &ReadCloser{NewReader(reader, templates), r}, nil
}
