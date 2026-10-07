package bsup

import (
	"io"

	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type ArrayEncoder struct {
	values  Encoder
	offsets *Uint32Encoder
	count   uint32
}

var _ Encoder = (*ArrayEncoder)(nil)

func NewArrayEncoder(cctx *Context, vec *vector.Array) *ArrayEncoder {
	return &ArrayEncoder{
		values:  NewEncoder(cctx, vec.Values),
		offsets: NewUint32Encoder(vec.Offsets),
	}
}

func (a *ArrayEncoder) Encode(group *errgroup.Group) {
	a.offsets.Encode(group)
	a.values.Encode(group)
}

func (a *ArrayEncoder) Emit(w io.Writer) error {
	if err := a.offsets.Emit(w); err != nil {
		return err
	}
	return a.values.Emit(w)
}

func (a *ArrayEncoder) Metadata(cctx *Context, off uint64) (uint64, ID) {
	off, lens := a.offsets.Segment(off)
	off, vals := a.values.Metadata(cctx, off)
	return off, cctx.enter(&Array{
		Length:  a.count,
		Lengths: lens,
		Values:  vals,
	})
}

type SetEncoder struct {
	ArrayEncoder
}

func NewSetEncoder(cctx *Context, vec *vector.Set) *SetEncoder {
	return &SetEncoder{
		ArrayEncoder{
			values:  NewEncoder(cctx, vec.Values),
			offsets: &Uint32Encoder{vals: vec.Offsets},
		},
	}
}

func (s *SetEncoder) Metadata(cctx *Context, off uint64) (uint64, ID) {
	off, id := s.ArrayEncoder.Metadata(cctx, off)
	array := cctx.Lookup(id).(*Array) // XXX this leaves a dummy node in the table
	return off, cctx.enter(&Set{
		Length:  array.Length,
		Lengths: array.Lengths,
		Values:  array.Values,
	})
}
