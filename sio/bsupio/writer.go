package bsupio

import (
	"io"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/bsup/rows"
)

func NewColumnWriter(w io.WriteCloser) *bsup.ColumnWriter {
	return bsup.NewColumnWriter(w)
}

func NewNewRowWriter(w io.WriteCloser) *bsup.RowWriter {
	return bsup.NewRowWriter(w)
}

// XXX RowWriter provides a wrapper to the old BSUP format encapsulated by
// the new framing design.  This is here because we'll integrate BSUP ROWS into
// BSUP in a future PR.
type RowWriter struct {
	*rows.Writer
}

func NewRowWriter(w io.WriteCloser) *RowWriter {
	return &RowWriter{rows.NewWriter(w)}
}
