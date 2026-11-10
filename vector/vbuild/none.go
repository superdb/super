package vbuild

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type nullBuilder struct {
	len uint32
}

func (n *nullBuilder) Write(vec vector.Any) {
	n.len += vec.Len()
}

func (n *nullBuilder) Build() vector.Any {
	return vector.NewNull(n.len)
}

type noneBuilder struct {
	len uint32
}

func (n *noneBuilder) Write(vec vector.Any) {
	n.len += vec.Len()
}

func (n *noneBuilder) Build() vector.Any {
	return vector.NewNone(n.len)
}

type emptyBuilder struct {
	typ super.Type
}

func (e *emptyBuilder) Write(vec vector.Any) {
	if vec.Len() != 0 {
		panic(e.typ)
	}
}

func (e *emptyBuilder) Build() vector.Any {
	return vector.NewEmpty(e.typ)
}
