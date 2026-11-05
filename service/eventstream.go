package service

import (
	"bytes"
	"fmt"
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/anyio"
)

type event struct {
	name  string
	sctx  *super.Context // type context of value
	value super.Value
}

type eventStreamWriter struct {
	body   io.Writer
	format string
}

func (e *eventStreamWriter) writeEvent(ev event) error {
	var buf bytes.Buffer
	w, err := anyio.NewWriter(ev.sctx, sio.NopCloser(&buf), anyio.WriterOpts{Format: e.format})
	if err != nil {
		return err
	}
	if err := w.Push(sbuf.Dematerialize(ev.sctx, ev.value)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.body, "event: %s\ndata: %s\n\n", ev.name, &buf)
	return err
}
