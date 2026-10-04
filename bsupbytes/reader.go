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

type Reader interface {
	Read() (any, error)
}

type ReadCloser interface {
	Reader
	io.Closer
}

type decoder struct {
	reader      sio.Reader
	unmarshaler *super.Unmarshaler
}

func NewReader(reader sio.Reader, templates []any) *decoder {
	u := super.NewUnmarshaler()
	u.Bind(templates...)
	return &decoder{
		reader:      reader,
		unmarshaler: u,
	}
}

func NewBytesReader(ctx context.Context, b []byte, templates []any) (Reader, error) {
	reader, err := bsupio.NewValueReader(ctx, super.NewContext(), bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return NewReader(reader, templates), nil
}

func (d *decoder) Read() (any, error) {
	val, err := d.reader.Read()
	if val == nil || err != nil {
		return nil, err
	}
	var action any
	if err := d.unmarshaler.Unmarshal(*val, &action); err != nil {
		return nil, err
	}
	return action, nil
}

func Get(ctx context.Context, engine storage.Engine, uri *storage.URI, templates []any) (ReadCloser, error) {
	r, err := engine.Get(ctx, uri)
	if err != nil {
		return nil, err
	}
	reader, err := bsupio.NewValueReader(ctx, super.NewContext(), r)
	return &struct {
		*decoder
		io.Closer
	}{
		NewReader(reader, templates),
		r,
	}, nil
}
