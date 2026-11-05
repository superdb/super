package bsup

import (
	"io"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/byteconv"
	"github.com/superdb/super/pkg/field"
	"golang.org/x/sync/errgroup"
)

type FloatEncoder struct {
	typ      super.Type
	vals     []float64
	min, max float64
	out      []byte
	fmt      uint8
}

func NewFloatEncoder(typ super.Type, vals []float64) *FloatEncoder {
	return &FloatEncoder{typ: typ, vals: vals}
}

func (f *FloatEncoder) Encode(group *errgroup.Group) {
	group.Go(func() error {
		bytes := slices.Clone(byteconv.ReinterpretSlice[byte](f.vals))
		var err error
		f.fmt, f.out, err = compressBuffer(bytes)
		return err
	})
	if len(f.vals) > 0 {
		group.Go(func() error {
			f.min, f.max = minMax(f.vals)
			return nil
		})
	}
}

func (f *FloatEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.WriteCount(uint32(len(f.vals)))
	w.Segment(Segment{
		Offset:            w.Offset(),
		MemLength:         uint64(len(f.vals)) * 8,
		Length:            uint64(len(f.out)),
		CompressionFormat: f.fmt,
	})
}

func (f *FloatEncoder) Stats(path field.Path, fn CollectStatsFunc) {
	if len(f.vals) > 0 {
		fn(path, super.NewFloat(f.typ, f.min), super.NewFloat(f.typ, f.max))
	}
}

func (f *FloatEncoder) Emit(w io.Writer) error {
	var err error
	if len(f.out) > 0 {
		_, err = w.Write(f.out)
	}
	return err
}
