package data

import (
	"context"
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup/oldbsup"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/bufwriter"
	"github.com/superdb/super/pkg/storage"
)

// Writer is a sio.Writer that writes a stream of sorted records into a
// data object.
type Writer struct {
	object      *Object
	byteCounter *writeCounter
	count       uint64
	writer      *oldbsup.Writer
	sortKey     order.SortKey
	first       bool
}

// NewWriter returns a writer for writing the data of a BSUP object.  We assume all records are
// non-volatile until Close as super.Values from the various record bodies are referenced across
// calls to Write.
func (o *Object) NewWriter(ctx context.Context, engine storage.Engine, path *storage.URI, sortKey order.SortKey) (*Writer, error) {
	out, err := engine.Put(ctx, o.SequenceURI(path))
	if err != nil {
		return nil, err
	}
	counter := &writeCounter{bufwriter.New(out), 0}
	return &Writer{
		object:      o,
		byteCounter: counter,
		writer:      oldbsup.NewWriter(counter),
		sortKey:     sortKey,
		first:       true,
	}, nil
}

func (w *Writer) Write(val super.Value) error {
	key := val.DerefPath(w.sortKey.Key).MissingAsNull()
	return w.WriteWithKey(key, val)
}

func (w *Writer) WriteWithKey(key, val super.Value) error {
	w.count++
	if err := w.writer.Write(val); err != nil {
		return err
	}
	if w.first {
		w.first = false
		w.object.Min.CopyFrom(key)
	}
	w.object.Max.CopyFrom(key)
	return nil
}

// Abort is called when an error occurs during write. Errors are ignored
// because the write error will be more informative and should be returned.
func (w *Writer) Abort() {
	w.writer.Close()
}

func (w *Writer) Close(ctx context.Context) error {
	if err := w.writer.Close(); err != nil {
		w.Abort()
		return err
	}
	w.object.Count = w.count
	w.object.Size = w.writer.Position()
	if w.sortKey.Order == order.Desc {
		w.object.Min, w.object.Max = w.object.Max, w.object.Min
	}
	return nil
}

func (w *Writer) BytesWritten() int64 {
	return w.byteCounter.size
}

func (w *Writer) RecordsWritten() uint64 {
	return w.count
}

// Object returns the Object written by the writer. This is only valid after
// Close() has returned a nil error.
func (w *Writer) Object() *Object {
	return w.object
}

type writeCounter struct {
	io.WriteCloser
	size int64
}

func (w *writeCounter) Write(b []byte) (int, error) {
	n, err := w.WriteCloser.Write(b)
	w.size += int64(n)
	return n, err
}
