package bsup

import (
	"io"

	"github.com/superdb/super/pkg/byteconv"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/bitvec"
	"golang.org/x/sync/errgroup"
)

type BoolEncoder struct {
	bits bitvec.Bits

	// created on Encode
	out      []byte
	fmt      uint8
	bytesLen uint64
}

func NewBoolEncoder(vec *vector.Bool) Encoder {
	return &BoolEncoder{bits: vec.Bits}
}

func (b *BoolEncoder) Encode(group *errgroup.Group) {
	group.Go(func() error {
		bytes := byteconv.ReinterpretSlice[byte](b.bits.GetBits())
		f, out, err := compressBuffer(bytes)
		if err != nil {
			return err
		}
		b.fmt = f
		b.out = out
		b.bytesLen = uint64(len(bytes))
		return nil
	})
}

func (b *BoolEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.WriteCount(b.bits.Len())
	w.Segment(Segment{
		Offset:            w.Offset(),
		Length:            uint64(len(b.out)),
		MemLength:         b.bytesLen,
		CompressionFormat: b.fmt,
	})
}

func (b *BoolEncoder) Stats(field.Path, CollectStatsFunc) {}

func (b *BoolEncoder) Emit(w io.Writer) error {
	_, err := w.Write(b.out)
	return err
}
