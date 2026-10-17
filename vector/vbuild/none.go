package vbuild

import (
	"github.com/superdb/super/vector"
)

type missingBuilder struct {
	len uint32
}

func (n *missingBuilder) Write(vec vector.Any) {
	n.len += vec.Len()
}

func (n *missingBuilder) Build() vector.Any {
	return vector.NewMissing(n.len)
}

type nullBuilder struct {
	len uint32
}

func (n *nullBuilder) Write(vec vector.Any) {
	n.len += vec.Len()
}

func (n *nullBuilder) Build() vector.Any {
	return vector.NewNull(n.len)
}
