package vbuild

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type anyBuilder struct {
	typ      *super.TypeSuper
	bytes    Builder
	subtypes superTypeBuilder
}

func newAnyBuilder(typ *super.TypeSuper) Builder {
	return &anyBuilder{typ: typ, bytes: newBytesBuilder(super.TypeBytes)}
}

func (a *anyBuilder) Write(vec vector.Any) {
	sup := vector.PushViewOrConst(vec).(*vector.Super)
	a.bytes.Write(sup.Values)
	a.subtypes.Write(sup.Subtypes)
}

func (a *anyBuilder) Build() vector.Any {
	return &vector.Super{
		Typ:      a.typ,
		Values:   a.bytes.Build(),
		Subtypes: a.subtypes.Build().(*vector.TypeValue),
	}
}
