package vhash

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type namedKey struct {
	typ *super.TypeNamed
	key
}

func newNamedKey(typ *super.TypeNamed) *namedKey {
	return &namedKey{typ: typ, key: newKey(typ.Type)}
}

func (k *namedKey) vector(sctx *super.Context) vector.Any {
	return vector.NewNamed(k.typ, k.key.vector(sctx))
}
