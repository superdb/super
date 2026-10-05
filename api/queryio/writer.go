package queryio

import (
	"io"
	"net/http"

	"github.com/superdb/super"
	"github.com/superdb/super/api"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/anyio"
	"github.com/superdb/super/sio/jsonio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type controlWriter interface {
	WriteControl(any) error
}

type Writer struct {
	sctx    *super.Context
	channel string
	start   nano.Ts
	writer  vio.PushCloser
	ctrl    bool
	flusher http.Flusher
}

func NewWriter(sctx *super.Context, w io.WriteCloser, format string, flusher http.Flusher, ctrl bool) (*Writer, error) {
	d := &Writer{
		sctx:    sctx,
		ctrl:    ctrl,
		start:   nano.Now(),
		flusher: flusher,
	}
	var err error
	switch format {
	case "bsup":
		d.writer = NewBSUPWriter(w)
	case "json":
		// A JSON response is always an array.
		d.writer = jsonio.NewArrayWriter(w)
	case "ndjson":
		d.writer = jsonio.NewWriter(w, jsonio.WriterOpts{})
	default:
		d.writer, err = anyio.NewWriter(sio.NopCloser(w), anyio.WriterOpts{
			Format: format,
		})
	}
	return d, err
}

func (w *Writer) Push(channel string, vec vector.Any) error {
	if w.channel != channel {
		w.channel = channel
		if err := w.WriteControl(api.QueryChannelSet{Channel: channel}); err != nil {
			return err
		}
	}
	return w.writer.Push(vec)
}

func (w *Writer) WriteChannelEnd(channel string) error {
	return w.WriteControl(api.QueryChannelEnd{Channel: channel})
}

func (w *Writer) WriteProgress(stats vio.Progress) error {
	v := api.QueryStats{
		StartTime:  w.start,
		UpdateTime: nano.Now(),
		Progress:   stats,
	}
	return w.WriteControl(v)
}

func (w *Writer) WriteError(err error) {
	w.WriteControl(api.QueryError{Error: err.Error()})
}

func (w *Writer) WriteControl(value any) error {
	if !w.ctrl {
		return nil
	}
	var err error
	if ctrl, ok := w.writer.(controlWriter); ok {
		err = ctrl.WriteControl(value)
		if w.flusher != nil {
			w.flusher.Flush()
		}
	}
	return err
}

func (w *Writer) Close() error {
	return w.writer.Close()
}
