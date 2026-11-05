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
	seen     map[groupType]struct{}
	types    [][]super.Type // by group
	partials [][]super.Type // by group
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
	f.types = append(f.types, make([][]super.Type, n-uint32(len(f.types)))...)
	f.partials = append(f.partials, make([][]super.Type, n-uint32(len(f.partials)))...)
}

func (f *fuse) Consume(groups []uint32, vec vector.Any) {
	if v, ok := vec.(*vector.NoRip); ok {
		vec = v.Any
	}
	// Fuse the types of the superdefused values (i.e., the subtypes of
	// any fusion values) as with the superfuse operator.  Note that vec
	// may be a view of the values in a group, so the view must be kept
	// since the subtypes of a fusion vary by slot.
	vec = vector.PushView(f.defuse.Eval(vec))
	if groups == nil {
		if d, ok := vec.(*vector.Dynamic); ok {
			for slot := range vec.Len() {
				f.consume(0, d.TypeOf(slot))
			}
			return
		}
		f.consume(0, vec.Type())
		return
	}
	if d, ok := vec.(*vector.Dynamic); ok {
		for slot, g := range groups {
			f.consume(g, d.TypeOf(uint32(slot)))
		}
		return
	}
	typ := vec.Type()
	for _, g := range groups {
		f.consume(g, typ)
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
	tags := make([]uint8, len(f.types))
	for g := range f.types {
		if len(f.types[g])+len(f.partials[g]) > 0 {
			tags[g] = 1
		}
	}
	return buildByTag(tags, 2, func(tag uint8, groups []uint32) vector.Any {
		if tag == 0 {
			n := uint32(len(f.types))
			if groups != nil {
				n = uint32(len(groups))
			}
			return vector.NewNull(n)
		}
		if groups == nil {
			groups = make([]uint32, len(f.types))
			for g := range groups {
				groups[g] = uint32(g)
			}
		}
		out := make([]super.Type, len(groups))
		for i, g := range groups {
			fuser := super.NewFuser(sctx)
			for _, typ := range f.partials[g] {
				fuser.Fuse(typ)
			}
			for _, typ := range f.types[g] {
				fuser.Fuse(typ)
			}
			if f.complete {
				out[i] = fuser.FusedType()
			} else {
				out[i] = fuser.SuperType()
			}
		}
		return vector.NewTypeValue(out)
	})
}

func (f *fuse) ConsumeAsPartial(groups []uint32, partial vector.Any) {
	kind := partial.Kind()
	if kind == vector.KindNull || kind == vector.KindMissing {
		return
	}
	if kind != vector.KindType {
		panic("fuse: partial not a type value")
	}
	for slot, g := range orZeros(groups, partial.Len()) {
		f.partials[g] = append(f.partials[g], vector.TypeValueValue(partial, uint32(slot)))
	}
}

func (f *fuse) ResultAsPartial(sctx *super.Context) vector.Any {
	return f.Result(sctx)
}
