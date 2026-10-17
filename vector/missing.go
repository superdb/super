package vector

import (
	"github.com/superdb/super"
	"github.com/superdb/super/scode"
)

type Missing struct {
	len uint32
}

func NewMissing(len uint32) *Missing {
	return &Missing{len: len}
}

func (*Missing) Kind() Kind {
	return KindMissing
}

func (m *Missing) Len() uint32 {
	return m.len
}

func (*Missing) Serialize(b *scode.Builder, _ uint32) {
	b.Append(nil)
}

func (*Missing) Type() super.Type {
	return super.TypeMissing
}
