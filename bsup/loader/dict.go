package loader

import (
	"sync"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

// Dict is a dictionary vector, whose locators are its kind, the locators of
// its values, the segment of its counts, and the segment of its index, whose
// length is the length of the dict.
type Dict struct {
	Node
	Values Shadow
	Counts bsup.Segment
	Index  bsup.Segment
	mu     sync.Mutex
	counts []uint32 // number of each entry indexed by dict offset
	index  []byte   // dict offset of each value in vector
}

func (d *Dict) project(loader *FrameLoader, projection field.Projection) vector.Any {
	if projection.TypesOnly() {
		// The type of a dict vector is the type of its values.
		return vector.NewTypeOnly(d.Values.project(loader, projection).Type(), d.Length)
	}
	index, counts := d.load(loader)
	return vector.NewDict(d.Values.project(loader, projection), index, counts)
}

func (d *Dict) load(loader *FrameLoader) ([]byte, []uint32) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.index == nil {
		d.index = readSegment(loader, d.Index)
		v, err := bsup.ReadUint32s(d.Counts, loader.frame)
		if err != nil {
			panic(err)
		}
		d.counts = v
	}
	return d.index, d.counts
}
