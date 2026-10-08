package loader

import (
	"net/netip"
	"sync"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type ip struct {
	mu   sync.Mutex
	meta *bsup.IP
	vals []netip.Addr
}

func newIP(meta *bsup.IP) *ip {
	return &ip{meta: meta}
}

func (i *ip) length() uint32 {
	return i.meta.Count
}

func (*ip) unmarshal(*bsup.Context, field.Projection) {}

func (i *ip) project(loader *loader, projection field.Projection) vector.Any {
	vec := vector.NewIP(i.load(loader))
	if len(projection) > 0 {
		return vector.NewWrappedError(loader.sctx, "'.': applied to non-record", vec)
	}
	return vec
}

func (i *ip) load(loader *loader) []netip.Addr {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.vals != nil {
		return i.vals
	}
	i.vals = make([]netip.Addr, i.meta.Count)
	table := loadBytesTable(loader, i.meta.Offsets, i.meta.Bytes)
	for k := range table.Len() {
		var ok bool
		if i.vals[k], ok = netip.AddrFromSlice(table.Bytes(k)); !ok {
			panic("malformed ip block")
		}

	}
	return i.vals
}
