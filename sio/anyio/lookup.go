package anyio

import (
	"context"
	"fmt"
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/arrowio"
	"github.com/superdb/super/sio/bsupio"
	"github.com/superdb/super/sio/csvio"
	"github.com/superdb/super/sio/jsonio"
	"github.com/superdb/super/sio/lineio"
	"github.com/superdb/super/sio/parquetio"
	"github.com/superdb/super/sio/supio"
	"github.com/superdb/super/sio/zeekio"
	"github.com/superdb/super/vector/vio"
)

func lookupReader(ctx context.Context, sctx *super.Context, r io.Reader, opts ReaderOpts) (vio.Puller, error) {
	switch opts.Format {
	case "arrows":
		r, err := arrowio.NewReader(sctx, r)
		if err != nil {
			return nil, err
		}
		return newVioPuller(sctx, r), nil
	case "bsup":
		return bsupio.NewReader(ctx, sctx, r, opts.Pushdown, opts.ConcurrentReaders)
	case "csv":
		return newVioPuller(sctx, csvio.NewReader(sctx, r, opts.CSV)), nil
	case "line":
		return newVioPuller(sctx, lineio.NewReader(r)), nil
	case "json":
		return jsonio.NewReader(context.Background(), sctx, r, opts.Pushdown, opts.ConcurrentReaders), nil
	case "parquet":
		return parquetio.NewReader(ctx, sctx, r, opts.Pushdown, opts.ConcurrentReaders)
	case "sup":
		return newVioPuller(sctx, supio.NewReader(sctx, r)), nil
	case "tsv":
		opts.CSV.Delim = '\t'
		return newVioPuller(sctx, csvio.NewReader(sctx, r, opts.CSV)), nil
	case "zeek":
		return newVioPuller(sctx, zeekio.NewReader(sctx, r)), nil
	}
	return nil, fmt.Errorf("no such format: \"%s\"", opts.Format)
}

func newVioPuller(sctx *super.Context, r sio.Reader) vio.Puller {
	puller := sbuf.NewDematerializer(sctx, sbuf.NewPuller(r))
	if typer, ok := r.(sio.Typer); ok {
		return struct {
			vio.Puller
			sio.Typer
		}{puller, typer}
	}
	return puller
}
