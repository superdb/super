package bsupio

import (
	"io"

	"github.com/superdb/super/bsup"
)

func NewColumnWriter(w io.WriteCloser) *bsup.ColumnWriter {
	return bsup.NewColumnWriter(w)
}

func NewNewRowWriter(w io.WriteCloser) *bsup.RowWriter {
	return bsup.NewRowWriter(w)
}
