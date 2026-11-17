package vbuild

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type anyBuilder struct {
	typ      *super.TypeFusion
	bytes    Builder
	subtypes fusionTypeBuilder
}

func newAnyBuilder(typ *super.TypeFusion) Builder {
	return &anyBuilder{typ: typ, bytes: newBytesBuilder(super.TypeBytes)}
}

func (a *anyBuilder) Write(vec vector.Any) {
	fusion := vector.PushViewOrConst(vec).(*vector.Fusion)
	a.bytes.Write(fusion.Values)
	a.subtypes.Write(fusion.Subtypes)
}

func (a *anyBuilder) Build() vector.Any {
	return &vector.Fusion{
		Typ:      a.typ,
		Values:   a.bytes.Build(),
		Subtypes: a.subtypes.Build().(*vector.TypeValue),
	}
}
