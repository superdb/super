package loader

import (
	"sync"

	"github.com/ronanh/intcomp"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/byteconv"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type uint_ struct {
	mu   sync.Mutex
	meta *bsup.Uint
	len  uint32
	vals []uint64
}

func newUint(bctx *bsup.Context, meta *bsup.Uint) *uint_ {
	return &uint_{meta: meta, len: meta.Len(bctx)}
}

func (u *uint_) length() uint32 {
	return u.len
}

func (*uint_) unmarshal(*bsup.Context, field.Projection) {}

func (u *uint_) project(loader *FrameLoader, projection field.Projection) vector.Any {
	if projection.TypesOnly() {
		return vector.NewTypeOnly(u.meta.Typ, u.len)
	}
	return vector.NewUint(u.meta.Typ, u.load(loader))
}

func (u *uint_) load(loader *FrameLoader) []uint64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.vals != nil {
		return u.vals
	}
	bytes := make([]byte, u.meta.Location.MemLength)
	if err := u.meta.Location.Read(loader.frame, bytes); err != nil {
		panic(err)
	}
	u.vals = intcomp.UncompressUint64(byteconv.ReinterpretSlice[uint64](bytes), nil)
	return u.vals
}
