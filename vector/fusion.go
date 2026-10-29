package vector

import (
	"github.com/superdb/super"
	"github.com/superdb/super/scode"
)

type Fusion struct {
	Typ    *super.TypeFusion
	Values Any
	// The TypeValue vectors are always created as typedefs, whether
	// loaded from BSUP, built from a fuse operation, built from JSON
	// etc.  They are only materialized into the query context when
	// absoluately necessary to perform less common operations that require
	// detailed types.  When coming from BSUP, the typedefs are lazily loaded
	// and often never even read from storage.
	Subtypes *TypeValue
}

var _ Any = (*Fusion)(nil)

func NewFusion(typ *super.TypeFusion, vals Any, subtypes []super.Type) *Fusion {
	return &Fusion{Typ: typ, Values: vals, Subtypes: NewTypeValue(subtypes)}
}

func NewFusionWithLoader(sctx *super.Context, typ *super.TypeFusion, loader TypesLoader, vals Any) *Fusion {
	return &Fusion{Typ: typ, Values: vals, Subtypes: NewTypeValueWithLoader(sctx, loader)}
}

func (*Fusion) Kind() Kind {
	return KindFusion
}

func (f *Fusion) Type() super.Type {
	return f.Typ
}

func (f *Fusion) Len() uint32 {
	return f.Values.Len()
}

func (f *Fusion) Serialize(b *scode.Builder, slot uint32) {
	b.BeginContainer()
	f.Values.Serialize(b, slot)
	f.Subtypes.Serialize(b, slot)
	b.EndContainer()
}

func Super(vec Any) Any {
	if vec.Kind() == KindFusion {
		switch vec := vec.(type) {
		case *View:
			return Pick(Super(vec.Any), vec.Index)
		case *Empty:
			return vec
		case *Fusion:
			return vec.Values
		}
	}
	return vec
}

func IsFusionAll(vec Any) bool {
	fusion, ok := vec.(*Fusion)
	return ok && fusion.Typ.Type == super.TypeAll
}
