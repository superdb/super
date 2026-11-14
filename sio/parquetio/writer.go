package parquetio

import (
	"fmt"
	"io"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/superdb/super"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/arrowio"
	"github.com/superdb/super/vector"
)

type Writer struct {
	*arrowio.Writer
}

func NewWriter(wc io.WriteCloser) *Writer {
	aw := arrowio.NewWriter(wc)
	aw.UnionsAsStructs = true
	aw.DropEmptyRecordFields = true
	aw.NewWriterFunc = func(w io.Writer, s *arrow.Schema) (arrowio.WriteCloser, error) {
		fw, err := pqarrow.NewFileWriter(s, sio.NopCloser(w), nil, pqarrow.DefaultWriterProps())
		if err != nil {
			return nil, fmt.Errorf("%w: %s", arrowio.ErrUnsupportedType, err)
		}
		if typ := aw.Type(); needsType(typ) {
			// Store the type so the reader can decode the structs back
			// into unions and restore dropped empty records.
			text, err := formatType(typ)
			if err != nil {
				return nil, err
			}
			if err := fw.AppendKeyValueMetadata(unionTypeKey, text); err != nil {
				return nil, err
			}
		}
		return fw, nil
	}
	return &Writer{aw}
}

func (w *Writer) Push(vec vector.Any) error {
	return sbuf.WriteVec(w, vec)
}

func (w *Writer) Write(val super.Value) error {
	if err := w.Writer.Write(val); err != nil {
		return parquetioError{err}
	}
	return nil
}

type parquetioError struct {
	err error
}

func (p parquetioError) Error() string {
	return "parquetio: " + strings.TrimPrefix(p.err.Error(), "arrowio: ")
}

func (p parquetioError) Unwrap() error { return p.err }
