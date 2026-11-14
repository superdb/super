package vector

import (
	"fmt"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sup"
)

// TypeOnly is a stand-in for a vector whose type is needed but whose data is
// not, e.g., when the BSUP loader projects a path whose values are referenced
// only as the argument to typeof or kind.  A TypeOnly vector has a type and
// a length but no data, so only the Type, Kind, and Len methods (and Pick)
// may be used.  It is a fatal bug for the data to be accessed, which would
// mean that the optimizer's demand analysis was wrong.
type TypeOnly struct {
	typ super.Type
	len uint32
}

var _ Any = (*TypeOnly)(nil)

func NewTypeOnly(typ super.Type, len uint32) *TypeOnly {
	return &TypeOnly{typ, len}
}

func (t *TypeOnly) Kind() Kind {
	return kindOf(t.typ)
}

func (t *TypeOnly) Type() super.Type {
	return t.typ
}

func (t *TypeOnly) Len() uint32 {
	return t.len
}

func (t *TypeOnly) Serialize(*scode.Builder, uint32) {
	panic(fmt.Sprintf("data of type-only vector of type %s accessed: bad demand analysis", sup.String(t.typ)))
}
