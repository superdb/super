package loader

import (
	"io"
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type typevalue struct {
	// We load the IDs separately from the types so that a shadow
	// of ids may be shared by multipe contexts.
	meta *bsup.TypeValue
	mu   sync.Mutex
	ids  []uint32
	len  uint32
}

func newTypeValue(bctx *bsup.Context, meta *bsup.TypeValue) *typevalue {
	return &typevalue{
		meta: meta,
		len:  meta.Len(bctx),
	}
}

func (t *typevalue) length() uint32 {
	return t.len
}

func (*typevalue) unmarshal(*bsup.Context, field.Projection) {}

func (t *typevalue) project(loader *FrameLoader, projection field.Projection) vector.Any {
	return vector.NewTypeValueWithLoader(loader.sctx, t.newLoader(loader))
}

func (t *typevalue) newLoader(loader *FrameLoader) *typesLoader {
	return &typesLoader{loader, t}
}

func (s *typevalue) loadIDs(r io.ReaderAt) []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ids == nil {
		ids, err := bsup.ReadUint32s(s.meta.Location, r)
		if err != nil {
			panic(err)
		}
		s.ids = ids
	}
	return s.ids
}

type typesLoader struct {
	loader *FrameLoader
	type_  *typevalue
}

var _ vector.TypesLoader = (*typesLoader)(nil)

func (s *typesLoader) Load() (*super.TypeIDMap, []uint32) {
	return s.loader.typeIDMap(), s.type_.loadIDs(s.loader.frame)
}
