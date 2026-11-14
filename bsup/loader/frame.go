package loader

import (
	"fmt"
	"iter"
	"sync"

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

	// root is the frame's root shadow, which is a *Dynamic if there's more
	// than one root vector.
	root Shadow

	// tmap maps the frame's typedefs to sctx for the loader and its type
	// vectors.
	tmapOnce sync.Once
	tmap     *super.TypeIDMap
	// vtmap maps the frame's vector typedefs to sctx.
	vtmapOnce sync.Once
	vtmap     *super.TypeIDMap
}

// typeIDMap returns the mapping of the frame's typedefs to sctx, loading the
// typedefs on first use.
func (f *FrameLoader) typeIDMap() *super.TypeIDMap {
	f.tmapOnce.Do(func() {
		f.tmap = super.NewTypeIDMap(f.sctx, f.frame.TypeDefs().LoadSubtypes())
	})
	return f.tmap
}

func (f *FrameLoader) lookupType(id uint32) super.Type {
	local, ok := f.typeIDMap().LookupID(id)
	if !ok {
		panic(fmt.Errorf("bad type ID %d in BSUP typedefs", id))
	}
	typ, err := f.sctx.LookupType(int(local))
	if err != nil {
		panic(err)
	}
	return typ
}

// vectorType returns the type in sctx of the type with ID id in the frame's
// vector typedefs table.
func (f *FrameLoader) vectorType(id uint32) super.Type {
	f.vtmapOnce.Do(func() {
		f.vtmap = super.NewTypeIDMap(f.sctx, f.frame.VectorTypeDefs())
	})
	local, ok := f.vtmap.LookupID(id)
	if !ok {
		panic(fmt.Errorf("bad type ID %d in BSUP vector typedefs", id))
	}
	typ, err := f.sctx.LookupType(int(local))
	if err != nil {
		panic(err)
	}
	return typ
}

// NewFrameLoader returns a loader of frame, whose locators it unmarshals into
// its shadows, or an error if the frame's locators or vector typedefs are
// malformed.
func NewFrameLoader(sctx *super.Context, frame *bsup.ColFrame) (*FrameLoader, error) {
	root, err := unmarshal(frame.VectorTypeDefs(), frame.Locators())
	if err != nil {
		return nil, err
	}
	return &FrameLoader{sctx: sctx, frame: frame, root: root}, nil
}

// Root returns the frame's root shadow.
func (f *FrameLoader) Root() Shadow {
	return f.root
}

func (f *FrameLoader) Len() uint32 {
	return f.Root().Len()
}

// Load returns the indicated projection of data in this BSUP object.
// If any required data is not memory resident, it will be fetched from
// storage and cached in memory so that subsequent calls run from memory.
// The vectors returned will have types from the provided sctx.  Multiple
// Load calls to the same object may run concurrently.
func (f *FrameLoader) Load(projection field.Projection) vector.Any {
	// Load all vector data into the in-memory shadow that is needed and not yet loaded
	// and return a new vector.Any using the data vectors in cache.  This may be called
	// concurrently on the same shadow and fine-grained locking insures that any given
	// data vector is loaded just once and such loads may be executed concurrently (even
	// when only one thread is calling load).  If paths is nil, then the entire value
	// is loaded.
	return project(f, f.Root(), projection)
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
		if d, ok := f.Root().(*Dynamic); ok {
			for _, value := range d.Values {
				if !yield(&shadowLoader{f, value}) {
					return
				}
			}
			return
		}
		yield(f)
	}
}

// shadowLoader loads a value of a dynamic root vector.
type shadowLoader struct {
	frameLoader *FrameLoader
	shadow      Shadow
}

func (s *shadowLoader) Len() uint32 {
	return s.shadow.Len()
}

func (s *shadowLoader) Load(projection field.Projection) vector.Any {
	return project(s.frameLoader, s.shadow, projection)
}

// Strings loads every string buffer in the frame (string columns, dict
// values, and string/complex consts) and returns them as a list. Only
// string data is read; other columns are not touched. Loaded tables are
// cached in the shadow, so a later Load reuses them.
func (f *FrameLoader) Strings() []string {
	c := &collector{loader: f}
	f.Root().strings(c)
	c.wg.Wait()
	return c.strings
}

func (f *FrameLoader) Search(expr vio.SearchEvaluator) bool {
	return expr.Eval(f.Strings())
}
