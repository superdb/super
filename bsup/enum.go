package bsup

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type EnumEncoder struct {
	*UintEncoder
	typ *super.TypeEnum
}

func NewEnumEncoder(vec *vector.Enum) *EnumEncoder {
	return &EnumEncoder{
		UintEncoder: &UintEncoder{typ: vec.Uint.Typ, vals: vec.Uint.Values},
		typ:         vec.Typ,
	}
}

// The locators of an enum are those of its uint values (see UintEncoder.Locate).
