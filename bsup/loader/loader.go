package loader

import (
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

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
type loader struct {
	cctx *bsup.Context
	sctx *super.Context
	r    io.ReaderAt
}

// Load all vector data into the in-memory shadow that is needed and not yet loaded
// and return a new vector.Any using the data vectors in cache.  This may be called
// concurrently on the same shadow and fine-grained locking insures that any given
// data vector is loaded just once and such loads may be executed concurrently (even
// when only one thread is calling load).  If paths is nil, then the entire value
// is loaded.  All of the projected paths in the shadow must have been properly
// unmarshaled before calling.
func (l *loader) load(projection field.Projection, s shadow) (vector.Any, error) {
	return s.project(l, projection), nil
}
