package loader

import (
	"sync"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

// Dynamic is the dynamic root vector of a frame with more than one root
// vector, whose locators are the type ID of each of its values, the segment of
// its tags, and the locators of each of its values.
type Dynamic struct {
	Node
	TypeIDs []uint32
	Tags    bsup.Segment
	Values  []Shadow
	mu      sync.Mutex
	tags    []uint32 // need not be loaded for unordered dynamics
}

func (d *Dynamic) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vecs := make([]vector.Any, 0, len(d.Values))
	for _, value := range d.Values {
		vecs = append(vecs, value.project(loader, projection))
	}
	return vector.NewDynamic(d.load(loader), vecs)
}

func (d *Dynamic) load(loader *FrameLoader) []uint32 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tags == nil {
		tags, err := bsup.ReadUint32s(d.Tags, loader.frame)
		if err != nil {
			panic(err)
		}
		d.tags = tags
	}
	return d.tags
}
