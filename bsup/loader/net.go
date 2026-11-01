package loader

import (
	"net/netip"
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type net struct {
	mu   sync.Mutex
	meta *bsup.Net
	vals []netip.Prefix
}

func newNet(meta *bsup.Net) *net {
	return &net{meta: meta}
}

func (n *net) length() uint32 {
	return n.meta.Count
}

func (*net) unmarshal(*bsup.Context, field.Projection) {}

func (n *net) project(loader *FrameLoader, projection field.Projection) vector.Any {
	if projection.TypesOnly() {
		return vector.NewTypeOnly(super.TypeNet, n.length())
	}
	return vector.NewNet(n.load(loader))
}

func (n *net) load(loader *FrameLoader) []netip.Prefix {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.vals != nil {
		return n.vals
	}
	n.vals = make([]netip.Prefix, n.meta.Count)
	table := loadBytesTable(loader, n.meta.Offsets, n.meta.Bytes)
	for k := range table.Len() {
		if err := n.vals[k].UnmarshalBinary(table.Bytes(k)); err != nil {
			panic(err)
		}

	}
	return n.vals
}
