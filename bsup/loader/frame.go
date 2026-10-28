package loader

import (
	"iter"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
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
	return &FrameLoader{
		sctx:  sctx,
		frame: frame,
		root:  newShadow(frame.Context(), frame.Root()),
	}
}

func (f *FrameLoader) Len() uint32 {
	return f.root.length()
}

// Load returns the indicated projection of data in this BSUP object.
// If any required data is not memory resident, it will be fetched from
// storage and cached in memory so that subsequent calls run from memory.
// The vectors returned will have types from the provided sctx.  Multiple
// Load calls to the same object may run concurrently.
func (f *FrameLoader) Load(projection field.Projection) vector.Any {
	f.root.unmarshal(f.frame.Context(), projection)
	// Load all vector data into the in-memory shadow that is needed and not yet loaded
	// and return a new vector.Any using the data vectors in cache.  This may be called
	// concurrently on the same shadow and fine-grained locking insures that any given
	// data vector is loaded just once and such loads may be executed concurrently (even
	// when only one thread is calling load).  If paths is nil, then the entire value
	// is loaded.  All of the projected paths in the shadow must have been properly
	// unmarshaled before calling.
	return f.root.project(f, projection)
}

type Loader interface {
	Len() uint32
	Load(field.Projection) vector.Any
}

// UnorderedLoaders iterates over loaders for the frame's root vector, producing
// a single loader unless the root vector is dynamic, in which case it produces
// one loader per underlying values vector.
func (f *FrameLoader) UnorderedLoaders() iter.Seq[Loader] {
	return func(yield func(Loader) bool) {
		if d, ok := f.root.(*dynamic); ok {
			for _, shadow := range d.shadows(f.frame.Context()) {
				if !yield(&shadowLoader{f, shadow}) {
					return
				}
			}
			return
		}
		yield(f)
	}
}

type shadowLoader struct {
	frameLoader *FrameLoader
	shadow      shadow
}

func (s *shadowLoader) Len() uint32 {
	return s.shadow.length()
}

func (s *shadowLoader) Load(projection field.Projection) vector.Any {
	s.shadow.unmarshal(s.frameLoader.frame.Context(), projection)
	return s.shadow.project(s.frameLoader, projection)
}

// Strings loads every string buffer in the frame (string columns, dict
// values, and string/complex consts) and returns them as a list. Only
// string data is read; other columns are not touched. Loaded tables are
// cached in the shadow, so a later Load reuses them.
func (f *FrameLoader) Strings() []string {
	f.root.unmarshal(f.frame.Context(), nil)
	c := &collector{loader: f}
	f.root.strings(c)
	c.wg.Wait()
	return c.strings
}

func (f *FrameLoader) Search(expr vio.SearchEvaluator) bool {
	return expr.Eval(f.Strings())
}
