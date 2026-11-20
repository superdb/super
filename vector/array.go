package vector

import (
	"github.com/superdb/super"
	"github.com/superdb/super/scode"
)

type Array struct {
	Typ     *super.TypeArray
	Offsets []uint32
	Values  Any
}

var _ Any = (*Array)(nil)

func NewArray(typ *super.TypeArray, offsets []uint32, values Any) *Array {
	return &Array{typ, offsets, values}
}

func (*Array) Kind() Kind {
	return KindArray
}

func (a *Array) Type() super.Type {
	return a.Typ
}

func (a *Array) Len() uint32 {
	return uint32(len(a.Offsets) - 1)
}

func (a *Array) Serialize(b *scode.Builder, slot uint32) {
	off := a.Offsets[slot]
	b.BeginContainer()
	for end := a.Offsets[slot+1]; off < end; off++ {
		a.Values.Serialize(b, off)
	}
	b.EndContainer()
}

func ContainerOffset(val Any, slot uint32) (uint32, uint32) {
	switch val := val.(type) {
	case *Array:
		return val.Offsets[slot], val.Offsets[slot+1]
	case *Set:
		return val.Offsets[slot], val.Offsets[slot+1]
	case *Map:
		return val.Offsets[slot], val.Offsets[slot+1]
	case *View:
		slot = val.Index[slot]
		return ContainerOffset(val.Any, slot)
	case *Const:
		return ContainerOffset(val.Any, 0)
	}
	panic(val)
}

func Inner(val Any) Any {
	switch val := val.(type) {
	case *Array:
		return val.Values
	case *Set:
		return val.Values
	case *Dict:
		return Inner(val.Any)
	case *View:
		return Inner(val.Any)
	case *Const:
		return Inner(val.Any)
	}
	panic(val)
}
