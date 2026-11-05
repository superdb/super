package vbuild

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type fusionBuilder struct {
	typ      *super.TypeFusion
	vals     Builder
	subtypes fusionTypeBuilder
}

func newFusionBuilder(typ *super.TypeFusion) *fusionBuilder {
	return &fusionBuilder{
		typ:  typ,
		vals: New(typ.Type),
	}
}

func (f *fusionBuilder) Write(vec vector.Any) {
	if vec.Len() == 0 {
		return
	}
	// A view of a fusion is pushed down to its values and subtypes.
	fusion := vector.PushView(vec).(*vector.Fusion)
	f.vals.Write(fusion.Values)
	f.subtypes.Write(fusion.Subtypes)
}

func (f *fusionBuilder) Build() vector.Any {
	return &vector.Fusion{
		Typ:      f.typ,
		Values:   f.vals.Build(),
		Subtypes: f.subtypes.Build().(*vector.TypeValue),
	}
}

// fusionTypeBuilder builds the subtypes of a fusion vector.  If the first
// subtypes written are foreign (i.e., IDs in a TypeDefs table that haven't
// been mapped into the query context, as for BSUP), the subtypes are merged
// into a new TypeDefs table so they're mapped into the query context only if
// needed.  Otherwise, the subtypes are type IDs in the query context.
type fusionTypeBuilder struct {
	sctx *super.Context
	defs *super.TypeDefsBuilder // non-nil if merging foreign subtypes
	ids  []uint32
	tmap *super.TypeIDMap // mapping of defs, created on Load
}

// Write appends subtypes, which are always a flat TypeValue (a view of a
// fusion's subtypes is picked when the view is pushed down).
func (t *fusionTypeBuilder) Write(subtypes *vector.TypeValue) {
	if t.sctx == nil {
		t.sctx = subtypes.Sctx()
		if subtypes.IsForeign() {
			t.defs = super.NewTypeDefsBuilder()
		}
	}
	if t.defs == nil {
		t.ids = append(t.ids, subtypes.IDs()...)
		return
	}
	defs, ids := subtypes.TypeIDs()
	merger := super.NewTypeDefsMerger(t.defs, defs)
	for _, id := range ids {
		t.ids = append(t.ids, merger.LookupID(id))
	}
}

func (t *fusionTypeBuilder) Build() vector.Any {
	if t.defs != nil {
		return vector.NewTypeValueWithLoader(t.sctx, t)
	}
	if t.ids == nil {
		t.ids = []uint32{}
	}
	return vector.NewTypeValueFromIDs(t.sctx, t.ids)
}

func (t *fusionTypeBuilder) Load() (*super.TypeIDMap, []uint32) {
	if t.tmap == nil {
		t.tmap = super.NewTypeIDMap(t.sctx, &t.defs.TypeDefs)
	}
	return t.tmap, t.ids
}
