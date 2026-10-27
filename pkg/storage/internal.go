package storage

import (
	"context"
	"io"
	"io/fs"
)

type OpenFunc func() (Reader, error)

type InternalEngine struct {
	openFuncs map[string]OpenFunc
}

func NewInternalEngine() *InternalEngine {
	return &InternalEngine{map[string]OpenFunc{}}
}

func (i *InternalEngine) AddOpenFunc(uri string, o OpenFunc) {
	i.openFuncs[uri] = o
}

// AddBytes makes b the contents of uri.  Each open of uri reads b from the
// start, and the reader supports io.ReaderAt and io.Seeker.
func (i *InternalEngine) AddBytes(uri string, b []byte) {
	i.openFuncs[uri] = func() (Reader, error) {
		return NewBytesReader(b), nil
	}
}

func (i *InternalEngine) AddReader(uri string, r io.Reader) {
	i.openFuncs[uri] = func() (Reader, error) {
		return &notSupportedReaderAt{io.NopCloser(r)}, nil
	}
}

func (i *InternalEngine) Get(_ context.Context, u *URI) (Reader, error) {
	o, ok := i.openFuncs[u.String()]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return o()
}

func (*InternalEngine) Put(context.Context, *URI) (io.WriteCloser, error) {
	return nil, ErrNotSupported
}

func (*InternalEngine) PutIfNotExists(context.Context, *URI, []byte) error {
	return ErrNotSupported

}
func (*InternalEngine) Delete(context.Context, *URI) error {
	return ErrNotSupported
}

func (*InternalEngine) DeleteByPrefix(context.Context, *URI) error {
	return ErrNotSupported
}

func (i *InternalEngine) Exists(_ context.Context, u *URI) (bool, error) {
	_, ok := i.openFuncs[u.String()]
	return ok, nil
}

func (i *InternalEngine) Size(_ context.Context, u *URI) (int64, error) {
	_, ok := i.openFuncs[u.String()]
	if !ok {
		return 0, fs.ErrNotExist
	}
	return 0, nil
}

func (*InternalEngine) List(context.Context, *URI) ([]Info, error) {
	return nil, ErrNotSupported
}
