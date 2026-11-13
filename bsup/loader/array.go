package loader

import (
	"fmt"
	"sync"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

// Array is an array vector, whose locators are its kind, count, the segment of
// its offsets, and the locators of its values.
type Array struct {
	Node
	Offsets bsup.Segment
	Values  Shadow
	mu      sync.Mutex
	offs    []uint32
}

func (a *Array) project(loader *FrameLoader, projection field.Projection) vector.Any {
	if projection.TypesOnly() {
		// The type of an array doesn't depend on its offsets but if the
		// elements contain a fusion value, then the data is needed to
		// compute the type of the superdefused value.
		if typ := loader.vectorType(a.TypeID); !hasFusion(typ) {
			return vector.NewTypeOnly(typ, a.Length)
		}
	}
	vec := a.Values.project(loader, nil)
	typ := loader.sctx.LookupTypeArray(vec.Type())
	return vector.NewArray(typ, a.load(loader), vec)
}

func (a *Array) load(loader *FrameLoader) []uint32 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.offs == nil {
		a.offs = loadOffsets(loader, a.Offsets, a.Length, a.Values.Len())
	}
	return a.offs
}

// loadOffsets loads the offsets of a list vector (e.g., an array) of length n
// whose values have length nvals.
func loadOffsets(loader *FrameLoader, s bsup.Segment, n, nvals uint32) []uint32 {
	offs, err := bsup.ReadUint32s(s, loader.frame)
	if err != nil {
		panic(err)
	}
	if len(offs) == 0 {
		offs = []uint32{0}
	}
	checkLen("list", len(offs)-1, n)
	if offs[len(offs)-1] != nvals {
		panic(fmt.Sprintf("BSUP loader: list vector ends at offset %d but has %d values", offs[len(offs)-1], nvals))
	}
	return offs
}

// Set is a set vector, whose locators are the same as an array's.
type Set struct {
	Array
}

func (s *Set) project(loader *FrameLoader, projection field.Projection) vector.Any {
	if projection.TypesOnly() {
		// See Array.project.
		if typ := loader.vectorType(s.TypeID); !hasFusion(typ) {
			return vector.NewTypeOnly(typ, s.Length)
		}
	}
	vec := s.Values.project(loader, nil)
	typ := loader.sctx.LookupTypeSet(vec.Type())
	return vector.NewSet(typ, s.load(loader), vec)
}
