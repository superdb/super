package loader

import (
	"sync"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/byteconv"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type float struct {
	mu   sync.Mutex
	meta *bsup.Float
	len  uint32
	vals []float64
}

func newFloat(cctx *bsup.Context, meta *bsup.Float) *float {
	return &float{meta: meta, len: meta.Len(cctx)}
}

func (f *float) length() uint32 {
	return f.len
}

func (*float) unmarshal(*bsup.Context, field.Projection) {}

func (f *float) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vec := vector.NewFloat(f.meta.Typ, f.load(loader))
	if len(projection) > 0 {
		return vector.NewWrappedError(loader.sctx, "'.': applied to non-record", vec)
	}
	return vec
}

func (f *float) load(loader *FrameLoader) []float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.vals != nil {
		return f.vals
	}
	bytes := make([]byte, f.meta.Location.MemLength)
	if err := f.meta.Location.Read(loader.frame, bytes); err != nil {
		panic(err)
	}
	f.vals = byteconv.ReinterpretSlice[float64](bytes)
	return f.vals
}
