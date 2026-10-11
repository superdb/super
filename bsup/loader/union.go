package loader

import (
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type union struct {
	mu   sync.Mutex
	meta *bsup.Union
	len  uint32
	// XXX we should store TagMap here so it doesn't have to be recomputed
	tags   []uint32
	values []shadow
}

func newUnion(cctx *bsup.Context, meta *bsup.Union) *union {
	return &union{
		meta:   meta,
		len:    meta.Len(cctx),
		values: make([]shadow, len(meta.Values)),
	}
}

func (u *union) length() uint32 {
	return u.len
}

func (u *union) unmarshal(cctx *bsup.Context, projection field.Projection) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for k, id := range u.meta.Values {
		if u.values[k] == nil {
			u.values[k] = newShadow(cctx, id)
		}
		u.values[k].unmarshal(cctx, projection)
	}
}

func (u *union) load(loader *FrameLoader) []uint32 {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.tags != nil {
		return u.tags
	}
	tags, err := bsup.ReadUint32s(u.meta.Tags, loader.frame)
	if err != nil {
		panic(err)
	}
	u.tags = tags
	return tags
}

func (u *union) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vecs := make([]vector.Any, 0, len(u.values))
	types := make([]super.Type, 0, len(u.values))
	for _, shadow := range u.values {
		vec := shadow.project(loader, projection)
		vecs = append(vecs, vec)
		types = append(types, vec.Type())
	}
	utyp := loader.sctx.MustLookupTypeUnion(types)
	tags := u.load(loader)
	if len(utyp.Types) == 2 {
		return vector.NewUnionFromRLE(utyp, tags, vecs)
	}
	return vector.NewUnion(utyp, tags, vecs)
}
