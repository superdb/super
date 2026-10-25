package anyio

import (
	"context"
	"io"
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
)

// Open uses engine to open path for reading.  path is a local file path or a
// URI whose scheme is understood by engine.
func Open(ctx context.Context, sctx *super.Context, engine storage.Engine, path string, opts ReaderOpts) (*sbuf.File, error) {
	src, err := NewSources(engine).Lookup(path)
	if err != nil {
		return nil, err
	}
	return src.Open(ctx, sctx, opts)
}

func NewFile(ctx context.Context, sctx *super.Context, rc io.ReadCloser, path string, opts ReaderOpts) (*sbuf.File, error) {
	r, err := GzipReader(rc)
	if err != nil {
		return nil, err
	}
	zr, err := NewReader(ctx, sctx, r, opts)
	if err != nil {
		return nil, err
	}
	return sbuf.NewFile(zr, rc, path), nil
}

func ReadFormatFromPath(path string) string {
	u, err := storage.ParseURI(path)
	if err != nil {
		return sio.FormatFromPath(strings.TrimSuffix(path, ".gz"))
	}
	switch storage.Scheme(u.Scheme) {
	case storage.HTTPScheme, storage.HTTPSScheme, storage.S3Scheme:
		// The format of a remote object often isn't evident from its name
		// (e.g., a REST API endpoint), so detect it.  Use the URL's path
		// so a query string doesn't hide the extension.
		if f := sio.FormatFromPath(strings.TrimSuffix(u.Path, ".gz")); f != "" {
			return f
		}
		return "auto"
	}
	if f := sio.FormatFromPath(strings.TrimSuffix(path, ".gz")); f != "" {
		return f
	}
	switch u.String() {
	case "/dev/fd/0", "/dev/stdin", "stdio:stdin":
		return "auto"
	}
	return ""
}
