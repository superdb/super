package anyio

import (
	"fmt"
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/sio/arrowio"
	"github.com/superdb/super/sio/csvio"
	"github.com/superdb/super/sio/dbio"
	"github.com/superdb/super/sio/jsonio"
	"github.com/superdb/super/sio/lineio"
	"github.com/superdb/super/sio/parquetio"
	"github.com/superdb/super/sio/supio"
	"github.com/superdb/super/sio/zeekio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type WriterOpts struct {
	Format    string
	SUPFusion bool
	BSUP      bsup.WriterOpts
	CSV       csvio.WriterOpts
	DB        dbio.WriterOpts
	JSON      jsonio.WriterOpts
	SUP       supio.WriterOpts
}

// NewWriter returns a writer of vectors in the format given by opts.  Vectors
// whose values may need to be defused for the format are defused in sctx,
// which must be the type context of the vectors written.
func NewWriter(sctx *super.Context, w io.WriteCloser, opts WriterOpts) (vio.PushCloser, error) {
	switch opts.Format {
	case "arrows":
		return newDefuser(sctx, arrowio.NewWriter(w)), nil
	case "bsup":
		return bsup.NewWriterWithOpts(w, opts.BSUP), nil
	case "csv":
		return newDefuser(sctx, csvio.NewWriter(w, opts.CSV)), nil
	case "db":
		return newDefuser(sctx, dbio.NewWriter(w, opts.DB)), nil
	case "json":
		return newDefuser(sctx, jsonio.NewWriter(w, opts.JSON)), nil
	case "line":
		return newDefuser(sctx, lineio.NewWriter(w)), nil
	case "null":
		return &nullWriter{}, nil
	case "parquet":
		return newDefuser(sctx, parquetio.NewWriter(w)), nil
	case "sup", "":
		w := vio.PushCloser(supio.NewWriter(w, opts.SUP))
		if !opts.SUPFusion {
			w = newDefuser(sctx, w)
		}
		return w, nil
	case "tsv":
		opts.CSV.Delim = '\t'
		return newDefuser(sctx, csvio.NewWriter(w, opts.CSV)), nil
	case "zeek":
		return newDefuser(sctx, zeekio.NewWriter(w)), nil
	default:
		return nil, fmt.Errorf("unknown format: %s", opts.Format)
	}
}

type defuser struct {
	vio.PushCloser
	defuse expr.Evaluator
}

// newDefuser returns a writer that defuses vectors in sctx, which must be the
// type context of the vectors since defusing compares their types with types
// it creates in sctx.
func newDefuser(sctx *super.Context, w vio.PushCloser) vio.PushCloser {
	return &defuser{PushCloser: w, defuse: expr.NewSuperDefuse(sctx)}
}

func (d *defuser) Push(vec vector.Any) error {
	label, ok := vec.(*vector.Labeled)
	if ok {
		vec = label.Any
	}
	if vec != nil {
		vec = d.defuse.Eval(vec)
	}
	if ok {
		vec = &vector.Labeled{Any: vec, Label: label.Label}
	}
	return d.PushCloser.Push(vec)
}

type nullWriter struct{}

func (*nullWriter) Push(vector.Any) error {
	return nil
}

func (*nullWriter) Close() error {
	return nil
}
