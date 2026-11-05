package bsup

import (
	"io"
	"net/netip"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type NetEncoder struct {
	vals     []netip.Prefix
	table    *BytesTableEncoder
	min, max netip.Prefix
}

func NewNetEncoder(vals []netip.Prefix) *NetEncoder {
	return &NetEncoder{
		vals: vals,
	}
}

func (n *NetEncoder) Encode(group *errgroup.Group) {
	group.Go(func() error {
		var bytes []byte
		offsets := []uint32{0}
		for _, net := range n.vals {
			var err error
			if bytes, err = net.AppendBinary(bytes); err != nil {
				panic(err)
			}
			offsets = append(offsets, uint32(len(bytes)))
		}
		n.table = NewBytesTableEncoder(vector.NewBytesTable(offsets, bytes))
		n.table.Encode(group)
		return nil
	})
	if len(n.vals) > 0 {
		group.Go(func() error {
			n.min, n.max = n.vals[0], n.vals[0]
			for _, v := range n.vals {
				if v.Compare(n.min) < 0 {
					n.min = v
				}
				if v.Compare(n.max) > 0 {
					n.max = v
				}
			}
			return nil
		})
	}
}

func (n *NetEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.WriteCount(uint32(len(n.vals)))
	n.table.locate(w)
}

func (i *NetEncoder) Stats(path field.Path, fn CollectStatsFunc) {
	if len(i.vals) > 0 {
		fn(path, super.NewNet(i.min), super.NewNet(i.max))
	}
}

func (n *NetEncoder) Emit(w io.Writer) error {
	return n.table.Emit(w)
}
