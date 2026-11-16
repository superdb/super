package storage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
)

type HTTPEngine struct{}

var _ Engine = (*HTTPEngine)(nil)

func NewHTTP() *HTTPEngine {
	return &HTTPEngine{}
}

func (*HTTPEngine) Get(ctx context.Context, u *URI) (Reader, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return nil, &notFoundError{resp.Status}
		}
		return nil, errors.New(resp.Status)
	}
	return &notSupportedReaderAt{resp.Body}, nil
}

// notFoundError reports an HTTP 404 by its status (e.g., "404 Not Found")
// rather than as "file does not exist" while still matching fs.ErrNotExist.
type notFoundError struct{ status string }

func (e *notFoundError) Error() string { return e.status }
func (*notFoundError) Unwrap() error   { return fs.ErrNotExist }

type notSupportedReaderAt struct{ io.ReadCloser }

func (*notSupportedReaderAt) ReadAt(_ []byte, _ int64) (int, error) { return 0, ErrNotSupported }

func (*HTTPEngine) Put(_ context.Context, u *URI) (io.WriteCloser, error) {
	return nil, ErrNotSupported
}

func (*HTTPEngine) PutIfNotExists(context.Context, *URI, []byte) error {
	return ErrNotSupported
}

func (*HTTPEngine) Delete(_ context.Context, u *URI) error {
	return ErrNotSupported
}

func (*HTTPEngine) DeleteByPrefix(_ context.Context, u *URI) error {
	return ErrNotSupported
}

func (*HTTPEngine) Size(_ context.Context, u *URI) (int64, error) {
	return 0, ErrNotSupported
}

func (*HTTPEngine) Exists(_ context.Context, u *URI) (bool, error) {
	return false, ErrNotSupported
}

func (*HTTPEngine) List(ctx context.Context, u *URI) ([]Info, error) {
	return nil, ErrNotSupported
}
