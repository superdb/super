package loader

import (
	"sync"

	"github.com/ronanh/intcomp"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/byteconv"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type int_ struct {
	mu   sync.Mutex
	meta *bsup.Int
	len  uint32
	vals []int64
}

func newInt(bctx *bsup.Context, meta *bsup.Int) *int_ {
	return &int_{meta: meta, len: meta.Len(bctx)}
}

func (i *int_) length() uint32 {
	return i.len
}

func (*int_) unmarshal(*bsup.Context, field.Projection) {}

func (i *int_) project(loader *FrameLoader, projection field.Projection) vector.Any {
	if projection.TypesOnly() {
		return vector.NewTypeOnly(i.meta.Typ, i.len)
	}
	return vector.NewInt(i.meta.Typ, i.load(loader))
}

func (i *int_) load(loader *FrameLoader) []int64 {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.vals != nil {
		return i.vals
	}
	bytes := make([]byte, i.meta.Location.MemLength)
	if err := i.meta.Location.Read(loader.frame, bytes); err != nil {
		panic(err)
	}
	i.vals = intcomp.UncompressInt64(byteconv.ReinterpretSlice[uint64](bytes), nil)
	return i.vals
}
