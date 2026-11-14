package agg

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

type fuse struct {
	defuse   *expr.SuperDefuse
	complete bool
	// seen holds the (group, type) pairs in types or partials.
	seen  map[groupType]struct{}
	types [][]super.Type // by group
}

type groupType struct {
	group uint32
	typ   super.Type
}

func newFuse(sctx *super.Context, complete bool) *fuse {
	return &fuse{
		defuse:   expr.NewSuperDefuse(sctx),
		complete: complete,
		seen:     make(map[groupType]struct{}),
	}
}

func (f *fuse) NoRip() bool { return true }

func (f *fuse) Resize(n uint32) {
	f.types = extend(f.types, n)
}

func (f *fuse) Consume(groups []uint32, vec vector.Any) {
	// Only the types of the superdefused values are needed, which
	// SuperDefuse.Types computes without the values' data (which might not
	// be loaded; see typeDemand in the optimizer).
	types := f.defuse.Types(vec)
	if c, ok := types.(*vector.Const); ok {
		typ := vector.TypeValueValue(c, 0)
		for _, g := range groups {
			f.consume(g, typ)
		}
		return
	}
	for slot, g := range groups {
		f.consume(g, vector.TypeValueValue(types, uint32(slot)))
	}
}

func (f *fuse) consume(g uint32, typ super.Type) {
	key := groupType{g, typ}
	if _, ok := f.seen[key]; !ok {
		f.seen[key] = struct{}{}
		f.types[g] = append(f.types[g], typ)
	}
}

func (f *fuse) Result(sctx *super.Context) vector.Any {
	types := make([]super.Type, 0, len(f.types))
	var nulls []uint32
	for g, gtyps := range f.types {
		if len(gtyps) == 0 {
			nulls = append(nulls, uint32(g))
			continue
		}
		fuser := super.NewFuser(sctx)
		for _, typ := range gtyps {
			fuser.Fuse(typ)
		}
		if f.complete {
			types = append(types, fuser.FusedType())
		} else {
			types = append(types, fuser.SuperType())
		}
	}
	return combineNulls(vector.NewTypeValue(sctx, types), nulls, uint32(len(f.types)))
}

func (f *fuse) ConsumeAsPartial(groups []uint32, partial vector.Any) {
	kind := partial.Kind()
	if kind == vector.KindNull || kind == vector.KindMissing {
		return
	}
	typeVec, ok := vector.PushView(partial).(*vector.TypeValue)
	if !ok {
		panic("fuse: partial not a type value")
	}
	types := typeVec.Types()
	for slot, g := range groups {
		f.consume(g, types[slot])
	}
}

func (f *fuse) ResultAsPartial(sctx *super.Context) vector.Any {
	return f.Result(sctx)
}
