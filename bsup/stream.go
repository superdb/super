package bsup

import (
	"io"

	"github.com/superdb/super"
)

type Stream struct {
	sctx   *super.Context
	reader io.Reader
}

func NewStream(sctx *super.Context, r io.Reader) *Stream {
	return &Stream{sctx: sctx, reader: r}
}

func (s *Stream) Next() (Frame, error) {
	panic("coming in next PR")
}

func (s *Stream) FusedType(sctx *super.Context) (super.Type, error) {
	return nil, nil
}
