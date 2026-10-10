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

func newInt(cctx *bsup.Context, meta *bsup.Int) *int_ {
	return &int_{meta: meta, len: meta.Len(cctx)}
}

func (i *int_) length() uint32 {
	return i.len
}

func (*int_) unmarshal(*bsup.Context, field.Projection) {}

func (i *int_) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vec := vector.NewInt(i.meta.Typ, i.load(loader))
	if len(projection) > 0 {
		return vector.NewWrappedError(loader.sctx, "'.': applied to non-record", vec)
	}
	return vec
}

func (i *int_) load(loader *FrameLoader) []int64 {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.vals != nil {
		return i.vals
	}
	bytes := make([]byte, i.meta.Location.MemLength)
	if err := i.meta.Location.Read(loader.frame.DataReader(), bytes); err != nil {
		panic(err)
	}
	i.vals = intcomp.UncompressInt64(byteconv.ReinterpretSlice[uint64](bytes), nil)
	return i.vals
}
