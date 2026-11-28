package vector

import (
	"github.com/superdb/super"
	"github.com/superdb/super/scode"
)

type Super struct {
	Typ    *super.TypeSuper
	Values Any
	// The TypeValue vectors are always created as typedefs, whether
	// loaded from BSUP, built from a fuse operation, built from JSON
	// etc.  They are only materialized into the query context when
	// absoluately necessary to perform less common operations that require
	// detailed types.  When coming from BSUP, the typedefs are lazily loaded
	// and often never even read from storage.
	Subtypes *TypeValue
}

var _ Any = (*Super)(nil)

func NewSuper(sctx *super.Context, typ *super.TypeSuper, vals Any, subtypes []super.Type) *Super {
	return &Super{Typ: typ, Values: vals, Subtypes: NewTypeValue(sctx, subtypes)}
}

func NewSuperWithLoader(sctx *super.Context, typ *super.TypeSuper, loader TypesLoader, vals Any) *Super {
	return &Super{Typ: typ, Values: vals, Subtypes: NewTypeValueWithLoader(sctx, loader)}
}

func (*Super) Kind() Kind {
	return KindSuper
}

func (f *Super) Type() super.Type {
	return f.Typ
}

func (f *Super) Len() uint32 {
	return f.Values.Len()
}

func (f *Super) Serialize(b *scode.Builder, slot uint32) {
	b.BeginContainer()
	f.Values.Serialize(b, slot)
	f.Subtypes.Serialize(b, slot)
	b.EndContainer()
}

// Fused returns the values of a super vector as encoded in its fused type
// and returns any other vector unchanged.
func Fused(vec Any) Any {
	if vec.Kind() == KindSuper {
		switch vec := vec.(type) {
		case *View:
			return Pick(Fused(vec.Any), vec.Index)
		case *Empty:
			return vec
		case *Super:
			return vec.Values
		}
	}
	return vec
}

func IsSuperAll(vec Any) bool {
	sup, ok := vec.(*Super)
	return ok && sup.Typ.Type == super.TypeAll
}
