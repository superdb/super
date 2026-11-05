package emitter

import (
	"context"
	"io"
	"os"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/bufwriter"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/pkg/terminal"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/anyio"
	"github.com/superdb/super/vector/vio"
)

func NewFileFromPath(ctx context.Context, sctx *super.Context, engine storage.Engine, path string, unbuffered bool, opts anyio.WriterOpts) (vio.PushCloser, error) {
	if path == "" {
		path = "stdio:stdout"
	}
	uri, err := storage.ParseURI(path)
	if err != nil {
		return nil, err
	}
	return NewFileFromURI(ctx, sctx, engine, uri, unbuffered, opts)
}

func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && terminal.IsTerminalFile(f)
}

func NewFileFromURI(ctx context.Context, sctx *super.Context, engine storage.Engine, path *storage.URI, unbuffered bool, opts anyio.WriterOpts) (vio.PushCloser, error) {
	f, err := engine.Put(ctx, path)
	if err != nil {
		return nil, err
	}
	wc := f
	if path.Scheme == "stdio" {
		// Don't close stdio in case we live inside something
		// that has multiple stdio users.
		wc = sio.NopCloser(f)
	}
	if !unbuffered && !IsTerminal(f) {
		// Don't buffer terminal output.
		wc = bufwriter.New(wc)
	}
	// On close, sio.WriteCloser.Close will close and flush the
	// downstream writer, which will flush the bufwriter here and,
	// in turn, close its underlying writer.
	return anyio.NewWriter(sctx, wc, opts)
}
