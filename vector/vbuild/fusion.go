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
	if view, ok := vec.(*vector.View); ok {
		fusion := view.Any.(*vector.Fusion)
		f.vals.Write(vector.Pick(fusion.Values, view.Index))
		f.subtypes.Write(vector.Pick(fusion.Subtypes, view.Index))
		return
	}
	fusion := vec.(*vector.Fusion)
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

func (t *fusionTypeBuilder) Write(vec vector.Any) {
	if t.sctx == nil {
		tv := typeValueOf(vec)
		t.sctx = tv.Sctx()
		if tv.IsForeign() {
			t.defs = super.NewTypeDefsBuilder()
		}
	}
	if t.defs != nil {
		t.writeTypeIDs(vec)
		return
	}
	switch vec := vec.(type) {
	case *vector.Const:
		id := typeValueOf(vec).IDs()[0]
		for range vec.Len() {
			t.ids = append(t.ids, id)
		}
	case *vector.Dict:
		ids := typeValueOf(vec).IDs()
		for _, slot := range vec.Index {
			t.ids = append(t.ids, ids[slot])
		}
	case *vector.View:
		ids := typeValueOf(vec).IDs()
		for _, slot := range vec.Index {
			t.ids = append(t.ids, ids[slot])
		}
	case *vector.TypeValue:
		t.ids = append(t.ids, vec.IDs()...)
	default:
		panic(vec)
	}
}

func (t *fusionTypeBuilder) writeTypeIDs(vec vector.Any) {
	switch vec := vec.(type) {
	case *vector.Const:
		types := vec.Any.(*vector.TypeValue)
		defs, ids := types.TypeIDs()
		id := super.NewTypeDefsMerger(t.defs, defs).LookupID(ids[0])
		for range vec.Len() {
			t.ids = append(t.ids, id)
		}
	case *vector.Dict:
		index := vec.Index
		types := vec.Any.(*vector.TypeValue)
		defs, ids := types.TypeIDs()
		merger := super.NewTypeDefsMerger(t.defs, defs)
		for _, slot := range index {
			t.ids = append(t.ids, merger.LookupID(ids[slot]))
		}
	case *vector.View:
		index := vec.Index
		types := vec.Any.(*vector.TypeValue)
		defs, ids := types.TypeIDs()
		merger := super.NewTypeDefsMerger(t.defs, defs)
		for _, slot := range index {
			t.ids = append(t.ids, merger.LookupID(ids[slot]))
		}
	case *vector.TypeValue:
		defs, ids := vec.TypeIDs()
		merger := super.NewTypeDefsMerger(t.defs, defs)
		for _, id := range ids {
			t.ids = append(t.ids, merger.LookupID(id))
		}
	default:
		panic(vec)
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

// typeValueOf returns the TypeValue underlying vec.
func typeValueOf(vec vector.Any) *vector.TypeValue {
	switch vec := vec.(type) {
	case *vector.Const:
		return typeValueOf(vec.Any)
	case *vector.Dict:
		return typeValueOf(vec.Any)
	case *vector.View:
		return typeValueOf(vec.Any)
	case *vector.TypeValue:
		return vec
	default:
		panic(vec)
	}
}
