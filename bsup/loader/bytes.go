package loader

import (
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type bytes struct {
	mu    sync.Mutex
	meta  *bsup.Bytes
	len   uint32
	table *vector.BytesTable
}

func newBytes(cctx *bsup.Context, meta *bsup.Bytes) *bytes {
	return &bytes{meta: meta, len: meta.Len(cctx)}
}

func (b *bytes) length() uint32 {
	return b.len
}

func (*bytes) unmarshal(*bsup.Context, field.Projection) {}

func (b *bytes) project(loader *FrameLoader, projection field.Projection) vector.Any {
	var vec vector.Any
	table := b.load(loader)
	switch b.meta.Typ.ID() {
	case super.IDString:
		vec = vector.NewString(table)
	case super.IDBytes:
		vec = vector.NewBytes(table)
	default:
		panic(b.meta.Typ)
	}
	if len(projection) > 0 {
		return vector.NewWrappedError(loader.sctx, "'.': applied to non-record", vec)
	}
	return vec
}

func (b *bytes) load(loader *FrameLoader) vector.BytesTable {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.table != nil {
		return *b.table
	}
	table := loadBytesTable(loader, b.meta.Offsets, b.meta.Bytes)
	b.table = &table
	return table
}

func loadBytesTable(loader *FrameLoader, offsets, bytes bsup.Segment) vector.BytesTable {
	offs, err := bsup.ReadUint32s(offsets, loader.frame.DataReader())
	if err != nil {
		panic(err)
	}
	if len(offs) == 0 {
		offs = []uint32{0}
	}
	b := make([]byte, bytes.MemLength)
	if err := bytes.Read(loader.frame.DataReader(), b); err != nil {
		panic(err)
	}
	return vector.NewBytesTable(offs, b)
}
