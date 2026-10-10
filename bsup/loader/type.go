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

func newTypeValue(cctx *bsup.Context, meta *bsup.TypeValue) *typevalue {
	return &typevalue{
		meta: meta,
		len:  meta.Len(cctx),
	}
}

func (t *typevalue) length() uint32 {
	return t.len
}

func (*typevalue) unmarshal(*bsup.Context, field.Projection) {}

func (t *typevalue) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vec := vector.NewTypeValueWithLoader(loader.sctx, t.newLoader(loader))
	if len(projection) > 0 {
		return vector.NewWrappedError(loader.sctx, "'.': applied to non-record", vec)
	}
	return vec
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

func (s *typesLoader) Load() (*super.TypeDefs, []uint32) {
	return s.loader.frame.Context().LoadSubtypes(), s.type_.loadIDs(s.loader.frame.DataReader())

}
