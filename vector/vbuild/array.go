package vbuild

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type arraySetBuilder struct {
	typ     super.Type
	inner   Builder
	offsets []uint32
	len     uint32
}

func newArraySetBuilder(typ super.Type) *arraySetBuilder {
	return &arraySetBuilder{
		typ:     typ,
		inner:   New(super.InnerType(typ)),
		offsets: []uint32{0},
	}
}

func (a *arraySetBuilder) Write(vec vector.Any) {
	n := vec.Len()
	if n == 0 {
		return
	}
	vec = vector.PushViewOrConst(vec)
	var offsets []uint32
	switch vec := vec.(type) {
	case *vector.Array:
		a.inner.Write(vec.Values)
		offsets = vec.Offsets
	case *vector.Set:
		a.inner.Write(vec.Values)
		offsets = vec.Offsets
	default:
		panic(vec)
	}
	for i := range n {
		a.len += offsets[i+1] - offsets[i]
		a.offsets = append(a.offsets, a.len)
	}
}

func (a *arraySetBuilder) Build() vector.Any {
	vec := a.inner.Build()
	switch typ := a.typ.(type) {
	case *super.TypeArray:
		return vector.NewArray(typ, a.offsets, vec)
	case *super.TypeSet:
		return vector.NewSet(typ, a.offsets, vec)
	default:
		panic(a.typ)
	}
}
