package vector

import (
	"net/netip"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
)

type Const struct {
	Any
	len uint32
}

func NewConst(vec Any, length uint32) *Const {
	return &Const{vec, length}
}

func NewConstUint(typ super.Type, v uint64, length uint32) *Const {
	vec := NewUint(typ, []uint64{v})
	return &Const{vec, length}
}
func NewConstInt(typ super.Type, v int64, length uint32) *Const {
	vec := NewInt(typ, []int64{v})
	return &Const{vec, length}
}
func NewConstFloat(typ super.Type, v float64, length uint32) *Const {
	vec := NewFloat(typ, []float64{v})
	return &Const{vec, length}
}

var (
	falseVec = NewFalse(1)
	trueVec  = NewTrue(1)
)

func NewConstBool(v bool, length uint32) *Const {
	if v {
		return &Const{trueVec, length}
	}
	return &Const{falseVec, length}
}

func NewConstBytes(v []byte, length uint32) *Const {
	vec := NewBytes(newBytesTableWithValue(v))
	return &Const{vec, length}
}

func newBytesTableWithValue(b []byte) BytesTable {
	offsets := []uint32{0, uint32(len(b))}
	return NewBytesTable(offsets, b)
}

func NewConstString(v string, length uint32) *Const {
	vec := NewString(newBytesTableWithValue([]byte(v)))
	return &Const{vec, length}
}

func NewConstIP(v netip.Addr, length uint32) *Const {
	vec := NewIP([]netip.Addr{v})
	return &Const{vec, length}
}

func NewConstNet(v netip.Prefix, length uint32) *Const {
	vec := NewNet([]netip.Prefix{v})
	return &Const{vec, length}
}

func NewConstType(sctx *super.Context, typ super.Type, length uint32) *Const {
	vec := NewTypeValue([]super.Type{typ})
	return &Const{vec, length}
}

func NewConstFromValue(sctx *super.Context, val super.Value, length uint32) *Const {
	b := NewValueBuilder(val.Type())
	b.Write(val.Bytes())
	return &Const{b.Build(sctx), length}
}

func (c *Const) Len() uint32 {
	return c.len
}

func (c *Const) Serialize(b *scode.Builder, slot uint32) {
	if slot >= c.len {
		panic([]uint32{slot, c.len})
	}
	c.Any.Serialize(b, 0)
}
