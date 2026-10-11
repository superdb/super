package loader

import (
	"sync"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/byteconv"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/bitvec"
)

type bool_ struct {
	mu   sync.Mutex
	meta *bsup.Bool
	bits *bitvec.Bits
}

func newBool(meta *bsup.Bool) *bool_ {
	return &bool_{meta: meta}
}

func (b *bool_) length() uint32 {
	return b.meta.Count
}

func (*bool_) unmarshal(*bsup.Context, field.Projection) {}

func (b *bool_) project(loader *FrameLoader, projection field.Projection) vector.Any {
	vec := vector.NewBool(b.load(loader))
	if len(projection) > 0 {
		return vector.NewWrappedError(loader.sctx, "'.': applied to non-record", vec)
	}
	return vec
}

func (b *bool_) load(loader *FrameLoader) bitvec.Bits {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bits != nil {
		return *b.bits
	}
	bytes := make([]byte, b.meta.Location.MemLength)
	if err := b.meta.Location.Read(loader.frame, bytes); err != nil {
		panic(err)
	}
	bits := bitvec.New(byteconv.ReinterpretSlice[uint64](bytes), b.meta.Count)
	b.bits = &bits
	return bits
}
