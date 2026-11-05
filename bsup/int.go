package bsup

import (
	"cmp"
	"io"

	"github.com/ronanh/intcomp"
	"github.com/superdb/super"
	"github.com/superdb/super/pkg/byteconv"
	"github.com/superdb/super/pkg/field"
	"golang.org/x/sync/errgroup"
)

type IntEncoder struct {
	typ  super.Type
	vals []int64

	// computed after encode is called.
	out []byte
	min int64
	max int64
}

func NewIntEncoder(typ super.Type, vals []int64) *IntEncoder {
	return &IntEncoder{
		typ:  typ,
		vals: vals,
	}
}

func (i *IntEncoder) Encode(group *errgroup.Group) {
	group.Go(func() error {
		compressed := intcomp.CompressInt64(i.vals, nil)
		i.out = byteconv.ReinterpretSlice[byte](compressed)
		return nil
	})
	if len(i.vals) > 0 {
		group.Go(func() error {
			i.min, i.max = minMax(i.vals)
			return nil
		})
	}
}

func (i *IntEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.WriteCount(uint32(len(i.vals)))
	w.Segment(Segment{
		Offset:            w.Offset(),
		MemLength:         uint64(len(i.out)),
		Length:            uint64(len(i.vals)) * 8,
		CompressionFormat: CompressionFormatNone,
	})
}

func (i *IntEncoder) Stats(path field.Path, stats CollectStatsFunc) {
	if len(i.vals) > 0 {
		stats(path, super.NewInt(i.typ, i.min), super.NewInt(i.typ, i.max))
	}
}

func (i *IntEncoder) Emit(w io.Writer) error {
	var err error
	if len(i.out) > 0 {
		_, err = w.Write(i.out)
	}
	return err
}

type UintEncoder struct {
	typ  super.Type
	vals []uint64

	// computed after encode is called
	min uint64
	max uint64
	out []byte
}

func (u *UintEncoder) Encode(group *errgroup.Group) {
	group.Go(func() error {
		compressed := intcomp.CompressUint64(u.vals, nil)
		u.out = byteconv.ReinterpretSlice[byte](compressed)
		return nil
	})
	if len(u.vals) > 0 {
		group.Go(func() error {
			u.min, u.max = minMax(u.vals)
			return nil
		})
	}
}

func minMax[T cmp.Ordered](vals []T) (T, T) {
	minVal, maxVal := vals[0], vals[0]
	for _, v := range vals {
		minVal = min(minVal, v)
		maxVal = max(maxVal, v)
	}
	return minVal, maxVal
}

func (u *UintEncoder) Segment(off uint64) (uint64, Segment) {
	loc := Segment{
		Offset:            off,
		MemLength:         uint64(len(u.out)),
		Length:            uint64(len(u.vals)) * 8,
		CompressionFormat: CompressionFormatNone,
	}
	return off + loc.MemLength, loc
}

func (u *UintEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.WriteCount(uint32(len(u.vals)))
	_, loc := u.Segment(w.Offset())
	w.Segment(loc)
}

func (u *UintEncoder) Stats(path field.Path, fn CollectStatsFunc) {
	if len(u.vals) > 0 {
		fn(path, super.NewUint(u.typ, u.min), super.NewUint(u.typ, u.max))
	}
}

func (u *UintEncoder) Emit(w io.Writer) error {
	var err error
	if len(u.out) > 0 {
		_, err = w.Write(u.out)
	}
	return err
}

type Uint32Encoder struct {
	vals     []uint32
	out      []byte
	bytesLen uint64
}

func NewUint32Encoder(vals []uint32) *Uint32Encoder {
	return &Uint32Encoder{vals: vals}
}

func (u *Uint32Encoder) Write(v uint32) {
	u.vals = append(u.vals, v)
}

func (u *Uint32Encoder) Append(vals []uint32) {
	u.vals = append(u.vals, vals...)
}

func (u *Uint32Encoder) Encode(group *errgroup.Group) {
	if len(u.vals) != 0 {
		group.Go(func() error {
			u.bytesLen = uint64(len(u.vals) * 4)
			compressed := intcomp.CompressUint32(u.vals, nil)
			u.out = byteconv.ReinterpretSlice[byte](compressed)
			return nil
		})
	}
}

func (u *Uint32Encoder) Emit(w io.Writer) error {
	var err error
	if len(u.out) > 0 {
		_, err = w.Write(u.out)
	}
	return err
}

func (u *Uint32Encoder) Segment(off uint64) (uint64, Segment) {
	len := uint64(len(u.out))
	return off + len, Segment{
		Offset:            off,
		MemLength:         len,
		Length:            u.bytesLen,
		CompressionFormat: CompressionFormatNone,
	}
}

func ReadUint32s(loc Segment, r io.ReaderAt) ([]uint32, error) {
	buf := make([]byte, loc.MemLength)
	if err := loc.Read(r, buf); err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, err
	}
	return intcomp.UncompressUint32(byteconv.ReinterpretSlice[uint32](buf), nil), nil
}
