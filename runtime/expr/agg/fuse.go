package agg

import (
	"fmt"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

type fuse struct {
	defuse   *expr.SuperDefuse
	complete bool
	seen     map[super.Type]struct{}
	types    []super.Type
	partials []super.Value
}

func newFuse(sctx *super.Context, complete bool) *fuse {
	return &fuse{
		defuse:   expr.NewSuperDefuse(sctx),
		complete: complete,
		seen:     make(map[super.Type]struct{}),
	}
}

func (f *fuse) NoRip() bool { return true }

func (f *fuse) Consume(vec vector.Any) {
	if v, ok := vec.(*vector.NoRip); ok {
		vec = v.Any
	}
	// Fuse the types of the superdefused values (i.e., the subtypes of
	// any fusion values) as with the superfuse operator.  Note that vec
	// may be a view of the values in a group, so the view must be kept
	// since the subtypes of a fusion vary by slot.
	vec = vector.PushView(f.defuse.Eval(vec))
	if d, ok := vec.(*vector.Dynamic); ok {
		for slot := range vec.Len() {
			f.consume(d.TypeOf(slot))
		}
		return
	}
	if vec.Type() == nil {
		fmt.Printf("vec panic %T\n", vec)
	}
	f.consume(vec.Type())
}

func (f *fuse) consume(typ super.Type) {
	if _, ok := f.seen[typ]; !ok {
		f.seen[typ] = struct{}{}
		f.types = append(f.types, typ)
	}
}

func (f *fuse) Result(sctx *super.Context) vector.Any {
	if len(f.types)+len(f.partials) == 0 {
		return vector.NewNull(1)
	}
	fuser := super.NewFuser(sctx)
	for _, p := range f.partials {
		typ, err := sctx.LookupByValue(p.Bytes())
		if err != nil {
			panic(fmt.Errorf("fuse: invalid partial value: %w", err))
		}
		fuser.Fuse(typ)
	}
	for _, typ := range f.types {
		fuser.Fuse(typ)
	}
	var typ super.Type
	if f.complete {
		typ = fuser.FusedType()
	} else {
		typ = fuser.SuperType()
	}
	return vector.NewTypeValue([]super.Type{typ})
}

func (f *fuse) ConsumeAsPartial(partial vector.Any) {
	kind := partial.Kind()
	if kind == vector.KindNull || kind == vector.KindMissing {
		return
	}
	if kind != vector.KindType {
		panic("fuse: partial not a type value")
	}
	for i := range partial.Len() {
		typ := vector.TypeValueValue(partial, i)
		f.partials = append(f.partials, super.NewValue(super.TypeType, super.EncodeTypeValue(typ)))
	}
}

func (f *fuse) ResultAsPartial(sctx *super.Context) vector.Any {
	return f.Result(sctx)
}
