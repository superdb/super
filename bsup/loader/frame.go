package loader

import (
	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

// FrameLoader is the interface to load a BSUP column frame from storage into
// memory and perform projections (or whole value reads) of the in-memory data.
// This is also suitable for one-pass use where the data is read on demand,
// used for processing, then discarded.  Frames maybe be accessed concurrently
// the super.Context in use is passed in for each vector constructed from
// its in-memory, sctx-independent shadow. This way one shadow can be used
// across multiple queries with different sctx.
type FrameLoader struct {
	frame *bsup.ColFrame
	root  shadow
}

func NewFrameLoader(frame *bsup.ColFrame) *FrameLoader {
	return &FrameLoader{frame: frame}
}

// Load returns the indicated projection of data in this BSUP object.
// If any required data is not memory resident, it will be fetched from
// storage and cached in memory so that subsequent calls run from memory.
// The vectors returned will have types from the provided sctx.  Multiple
// Load calls to the same object may run concurrently.
func (f *FrameLoader) Load(sctx *super.Context, projection field.Projection) (vector.Any, error) {
	cctx := f.frame.Context()
	f.root = newShadow(cctx, f.frame.Root())
	f.root.unmarshal(cctx, projection)
	loader := &loader{cctx, sctx, f.frame.DataReader()}
	return loader.load(projection, f.root)
}

// LoadUnordered is like Load, but if o's root vector is dynamic,
// LoadUnordered returns the underlying values vectors instead of a
// vector.Dynamic.
func (f *FrameLoader) LoadUnordered(vecs []vector.Any, sctx *super.Context, projection field.Projection) ([]vector.Any, error) {
	cctx := f.frame.Context()
	f.root = newShadow(cctx, f.frame.Root())
	f.root.unmarshal(cctx, projection)
	loader := &loader{cctx: cctx, sctx: sctx, r: f.frame.DataReader()}
	if d, ok := f.root.(*dynamic); ok {
		return d.projectUnordered(vecs, loader, projection), nil
	}
	vec, err := loader.load(projection, f.root)
	if err != nil {
		return nil, err
	}
	return append(vecs, vec), nil
}
