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

func newDict(cctx *bsup.Context, meta *bsup.Dict) *dict {
	return &dict{meta: meta, len: meta.Len(cctx)}
}

func (d *dict) length() uint32 {
	return d.len
}

func (d *dict) unmarshal(cctx *bsup.Context, projection field.Projection) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.values == nil {
		d.values = newShadow(cctx, d.meta.Values)
	}
	d.values.unmarshal(cctx, projection)
}

func (d *dict) project(loader *FrameLoader, projection field.Projection) vector.Any {
	index, counts := d.load(loader)
	vec := vector.NewDict(d.values.project(loader, projection), index, counts)
	if len(projection) > 0 {
		return vector.NewWrappedError(loader.sctx, "'.': applied to non-record", vec)
	}
	return vec
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
