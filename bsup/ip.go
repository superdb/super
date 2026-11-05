package bsup

import (
	"io"
	"net/netip"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type IPEncoder struct {
	vals     []netip.Addr
	table    *BytesTableEncoder
	min, max netip.Addr
}

func NewIPEncoder(vals []netip.Addr) *IPEncoder {
	return &IPEncoder{
		vals: vals,
	}
}

func (i *IPEncoder) Encode(group *errgroup.Group) {
	group.Go(func() error {
		var bytes []byte
		offsets := []uint32{0}
		for _, ip := range i.vals {
			var err error
			if bytes, err = ip.AppendBinary(bytes); err != nil {
				panic(err)
			}
			offsets = append(offsets, uint32(len(bytes)))
		}
		i.table = NewBytesTableEncoder(vector.NewBytesTable(offsets, bytes))
		i.table.Encode(group)
		return nil
	})
	if len(i.vals) > 0 {
		group.Go(func() error {
			i.min, i.max = i.vals[0], i.vals[0]
			for _, v := range i.vals {
				if v.Compare(i.min) < 0 {
					i.min = v
				}
				if v.Compare(i.max) > 0 {
					i.max = v
				}
			}
			return nil
		})
	}
}

func (i *IPEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.WriteCount(uint32(len(i.vals)))
	i.table.locate(w)
}

func (i *IPEncoder) Stats(path field.Path, fn CollectStatsFunc) {
	if len(i.vals) > 0 {
		fn(path, super.NewIP(i.min), super.NewIP(i.max))
	}
}

func (i *IPEncoder) Emit(w io.Writer) error {
	return i.table.Emit(w)
}
