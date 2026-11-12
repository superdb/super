package vhash

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

// typeKey is a key of type values, which are stored as their type IDs in the
// query context.
type typeKey struct {
	*numericKey[uint32]
}

func newTypeKey() *typeKey {
	return &typeKey{
		numericKey: &numericKey[uint32]{
			valuesOf: func(vec vector.Any) []uint32 {
				return vec.(*vector.TypeValue).IDs()
			},
		},
	}
}

func (k *typeKey) vector(sctx *super.Context) vector.Any {
	return vector.NewTypeValueFromIDs(sctx, k.vals)
}
