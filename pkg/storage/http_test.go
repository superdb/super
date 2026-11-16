package storage

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPNotFound(t *testing.T) {
	s := httptest.NewServer(http.NotFoundHandler())
	defer s.Close()
	u, err := ParseURI(s.URL)
	require.NoError(t, err)
	_, err = NewHTTP().Get(context.Background(), u)
	require.ErrorIs(t, err, fs.ErrNotExist)
	require.EqualError(t, err, "404 Not Found")
}
