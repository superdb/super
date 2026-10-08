package loader

import (
	"sync"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type set struct {
	mu     sync.Mutex
	meta   *bsup.Set
	len    uint32
	offs   []uint32
	values shadow
}

func newSet(cctx *bsup.Context, meta *bsup.Set) *set {
	return &set{meta: meta, len: meta.Len(cctx)}
}

func (s *set) length() uint32 {
	return s.len
}

func (s *set) unmarshal(cctx *bsup.Context, projection field.Projection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = newShadow(cctx, s.meta.Values)
	}
	s.values.unmarshal(cctx, projection)
}

func (s *set) project(loader *loader, projection field.Projection) vector.Any {
	vec := s.values.project(loader, nil)
	typ := loader.sctx.LookupTypeSet(vec.Type())
	offs := s.load(loader)
	if len(offs) == 0 {
		offs = []uint32{0}
	}
	return vector.NewSet(typ, offs, vec)
}

func (s *set) load(loader *loader) []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.offs != nil {
		return s.offs
	}
	offs, err := bsup.ReadUint32s(s.meta.Lengths, loader.r)
	if err != nil {
		panic(err)
	}
	s.offs = offs
	return offs
}
