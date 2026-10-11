package loader

import (
	"io"
	"sync"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/bitvec"
)

type dynamic struct {
	mu     sync.Mutex
	meta   *bsup.Dynamic
	tags   []uint32 // need not be loaded for unordered dynamics
	values []shadow
}

func newDynamic(meta *bsup.Dynamic) *dynamic {
	return &dynamic{meta: meta, values: make([]shadow, len(meta.Values))}
}

func (d *dynamic) length() uint32 {
	return d.meta.Length
}

func (d *dynamic) unmarshal(cctx *bsup.Context, projection field.Projection) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for k := range d.values {
		if d.values[k] == nil {
			d.values[k] = newShadow(cctx, d.meta.Values[k])
		}
		d.values[k].unmarshal(cctx, projection)
	}
}

func (d *dynamic) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vecs := make([]vector.Any, 0, len(d.values))
	for _, shadow := range d.values {
		vecs = append(vecs, shadow.project(loader, projection))
	}
	tags, _ := d.load(loader.frame)
	return vector.NewDynamic(tags, vecs)
}

func (d *dynamic) load(r io.ReaderAt) ([]uint32, bitvec.Bits) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tags != nil {
		return d.tags, bitvec.Zero
	}
	tags, err := bsup.ReadUint32s(d.meta.Tags, r)
	if err != nil {
		panic(err)
	}
	d.tags = tags
	return tags, bitvec.Zero
}

func (d *dynamic) projectUnordered(vecs []vector.Any, loader *FrameLoader, projection field.Projection) []vector.Any {
	for _, shadow := range d.values {
		vecs = append(vecs, shadow.project(loader, projection))
	}
	return vecs
}
