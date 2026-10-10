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
	sctx  *super.Context
	frame *bsup.ColFrame
	root  shadow
}

func NewFrameLoader(sctx *super.Context, frame *bsup.ColFrame) *FrameLoader {
	cctx := frame.Context()
	return &FrameLoader{
		sctx:  sctx,
		frame: frame,
		root:  newShadow(cctx, frame.Root()),
	}
}

// XXX
// loader handles loading vector data on demand for only the fields needed
// as specified in the projection.  The load operation is implemented simply
// by calling the project method on a shadow.  All data for that shadow
// will be loaded from this point in the value hierarchy possibly pruned by the
// projection argument (nil projection implies load the whole value).
//
// The sctx passed into the loader is dynamic and comes from each query context that
// uses the vcache.  No sctx types are stored in the shadow (except for primitive types
// in shadowed vector.Any primitives that are shared).  We otherwise allocate all
// vector.Any super.Types using the passed-in sctx.

// Load returns the indicated projection of data in this BSUP object.
// If any required data is not memory resident, it will be fetched from
// storage and cached in memory so that subsequent calls run from memory.
// The vectors returned will have types from the provided sctx.  Multiple
// Load calls to the same object may run concurrently.
func (f *FrameLoader) Load(sctx *super.Context, projection field.Projection) (vector.Any, error) {
	f.root.unmarshal(f.frame.Context(), projection)
	// Load all vector data into the in-memory shadow that is needed and not yet loaded
	// and return a new vector.Any using the data vectors in cache.  This may be called
	// concurrently on the same shadow and fine-grained locking insures that any given
	// data vector is loaded just once and such loads may be executed concurrently (even
	// when only one thread is calling load).  If paths is nil, then the entire value
	// is loaded.  All of the projected paths in the shadow must have been properly
	// unmarshaled before calling.
	return f.root.project(f, projection), nil //XXX always nil
}

// LoadUnordered is like Load, but if o's root vector is dynamic,
// LoadUnordered returns the underlying values vectors instead of a
// vector.Dynamic.
func (f *FrameLoader) LoadUnordered(vecs []vector.Any, sctx *super.Context, projection field.Projection) ([]vector.Any, error) {
	cctx := f.frame.Context()
	f.root = newShadow(cctx, f.frame.Root())
	f.root.unmarshal(cctx, projection)
	if d, ok := f.root.(*dynamic); ok {
		return d.projectUnordered(vecs, f, projection), nil
	}
	vec := f.root.project(f, projection)
	return append(vecs, vec), nil
}
