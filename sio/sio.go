package sio

import (
	"context"
	"io"
	"path/filepath"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/vector/vio"
)

func Extension(format string) string {
	switch format {
	case "bsup":
		return ".bsup"
	case "csv":
		return ".csv"
	case "json":
		return ".json"
	case "parquet":
		return ".parquet"
	case "sup":
		return ".sup"
	case "table":
		return ".tbl"
	case "zeek":
		return ".log"
	default:
		return ""
	}
}

func FormatFromPath(path string) string {
	switch filepath.Ext(path) {
	case ".arrows":
		return "arrows"
	case ".bsup":
		return "bsup"
	case ".csv":
		return "csv"
	case ".json", ".jsonl", ".ndjson":
		return "json"
	case ".parquet":
		return "parquet"
	case ".sup":
		return "sup"
	case ".text", ".txt":
		return "line"
	case ".tsv":
		return "tsv"
	default:
		return ""
	}
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error { return nil }

// NopCloser returns a WriteCloser with a no-op Close method wrapping
// the provided Writer w.
func NopCloser(w io.Writer) io.WriteCloser {
	return nopCloser{w}
}

// Reader wraps the Read method.
//
// Read returns the next value and a nil error, a nil value and the next error,
// or a nil value and nil error to indicate that no values remain.
//
// Read never returns a non-nil value and non-nil error together, and it never
// returns io.EOF.
//
// Implementations retain ownership of val and val.Bytes, and a subsequent Read
// may overwrite them.  Clients that wish to use val or val.Bytes after the next
// Read must make a copy.
type Reader interface {
	Read() (val *super.Value, err error)
}

// Writer wraps the Write method.
//
// Implementations must not retain val or val.Bytes.
type Writer interface {
	Write(val super.Value) error
}

type Typer interface {
	Type() (super.Type, error)
}

type ReadCloser interface {
	Reader
	io.Closer
}

type WriteCloser interface {
	Writer
	io.Closer
}

func NewReadCloser(r Reader, c io.Closer) ReadCloser {
	return extReadCloser{r, c}
}

type extReadCloser struct {
	Reader
	io.Closer
}

func NopReadCloser(r Reader) ReadCloser {
	return nopReadCloser{r}
}

type nopReadCloser struct {
	Reader
}

func (nopReadCloser) Close() error { return nil }

// ConcatReader returns a Reader that is the logical concatenation of readers,
// which are read sequentially.  Its Read methed returns any non-nil error
// returned by a reader and returns end of stream after all readers have
// returned end of stream.
func ConcatReader(readers ...Reader) Reader {
	if len(readers) == 1 {
		return readers[0]
	}
	return &concatReader{slices.Clone(readers)}
}

type concatReader struct {
	readers []Reader
}

func (c *concatReader) Read() (*super.Value, error) {
	for len(c.readers) > 0 {
		rec, err := c.readers[0].Read()
		if rec != nil || err != nil {
			return rec, err
		}
		c.readers = c.readers[1:]
	}
	return nil, nil
}

func MultiWriter(writers ...Writer) Writer {
	return &multiWriter{slices.Clone(writers)}
}

type multiWriter struct {
	writers []Writer
}

func (m *multiWriter) Write(rec super.Value) error {
	for _, w := range m.writers {
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	return nil
}

// Copy copies src to dst a la io.Copy.
func Copy(dst Writer, src Reader) error {
	return CopyWithContext(context.Background(), dst, src)
}

func CopyWithContext(ctx context.Context, dst Writer, src Reader) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rec, err := src.Read()
		if err != nil || rec == nil {
			return err
		}
		if err := dst.Write(*rec); err != nil {
			return err
		}
	}
}

func CloseReaders(readers []vio.PullCloser) error {
	var err error
	for _, reader := range readers {
		if e := reader.Close(); err == nil {
			err = e
		}
	}
	return err
}
