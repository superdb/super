package loader

import (
	"sync"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type dict struct {
	mu     sync.Mutex
	meta   *bsup.Dict
	len    uint32
	values shadow
	counts []uint32 // number of each entry indexed by dict offset
	index  []byte   // dict offset of each value in vector
}

func newDict(bctx *bsup.Context, meta *bsup.Dict) *dict {
	return &dict{meta: meta, len: meta.Len(bctx)}
}

func (d *dict) length() uint32 {
	return d.len
}

func (d *dict) unmarshal(bctx *bsup.Context, projection field.Projection) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.values == nil {
		d.values = newShadow(bctx, d.meta.Values)
	}
	d.values.unmarshal(bctx, projection)
}

func (d *dict) project(loader *FrameLoader, projection field.Projection) vector.Any {
	if projection.TypesOnly() {
		// The type of a dict vector is the type of its values.
		return vector.NewTypeOnly(d.values.project(loader, projection).Type(), d.len)
	}
	index, counts := d.load(loader)
	return vector.NewDict(d.values.project(loader, projection), index, counts)
}

func (d *dict) load(loader *FrameLoader) ([]byte, []uint32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.index = make([]byte, d.meta.Index.MemLength)
	if err := d.meta.Index.Read(loader.frame, d.index); err != nil {
		panic(err)
	}
	v, err := bsup.ReadUint32s(d.meta.Counts, loader.frame)
	if err != nil {
		panic(err)
	}
	d.counts = v
	return d.index, d.counts
}
