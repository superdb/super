package function

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
)

type Defuse struct {
	sctx      *super.Context
	downcast  *downcast
	hasFusion FusionChecker
}

func NewDefuse(sctx *super.Context) *Defuse {
	d := &Defuse{
		sctx:     sctx,
		downcast: &downcast{sctx: sctx},
	}
	d.downcast.defuser = d
	return d
}

func (d *Defuse) Call(args []super.Value) super.Value {
	return d.eval(args[0])
}

func (d *Defuse) eval(in super.Value) super.Value {
	if !d.hasFusion.Check(in.Type()) {
		return in
	}
	switch typ := in.Type().(type) {
	case *super.TypeRecord:
		var fields []super.Field
		var b scode.Builder
		b.BeginContainer()
		it := in.Bytes().Iter()
		for _, f := range typ.Fields {
			bytes := it.Next()
			val := d.eval(super.NewValue(f.Type, bytes))
			b.Append(val.Bytes())
			fields = append(fields, super.NewField(f.Name, val.Type()))
		}
		b.EndContainer()
		return super.NewValue(d.sctx.MustLookupTypeRecord(fields), b.Bytes().Body())
	case *super.TypeArray:
		elems := d.parseArrayOrSet(typ.Type, in.Bytes())
		if len(elems) == 0 {
			typ := d.sctx.LookupTypeArray(super.TypeNone)
			return super.NewValue(typ, nil)
		}
		elemType, bytes := d.unify(elems)
		return super.NewValue(d.sctx.LookupTypeArray(elemType), bytes)
	case *super.TypeSet:
		elems := d.parseArrayOrSet(typ.Type, in.Bytes())
		if len(elems) == 0 {
			typ := d.sctx.LookupTypeSet(super.TypeNone)
			return super.NewValue(typ, nil)
		}
		elemType, bytes := d.unify(elems)
		return super.NewValue(d.sctx.LookupTypeSet(elemType), bytes)
	case *super.TypeMap:
		var keys, vals []super.Value
		for it := in.Bytes().Iter(); !it.Done(); {
			keys = append(keys, super.NewValue(typ, it.Next()))
			vals = append(vals, super.NewValue(typ, it.Next()))
		}
		keyType := d.unifyType(keys)
		valType := d.unifyType(vals)
		var b scode.Builder
		for k, key := range keys {
			if u, ok := keyType.(*super.TypeUnion); ok {
				super.BuildUnion(&b, u.TagOf(u), key.Bytes())
			} else {
				b.Append(key.Bytes())
			}
			val := vals[k]
			if u, ok := valType.(*super.TypeUnion); ok {
				super.BuildUnion(&b, u.TagOf(u), val.Bytes())
			} else {
				b.Append(val.Bytes())
			}
		}
		return super.NewValue(d.sctx.LookupTypeMap(keyType, valType), b.Bytes())
	case *super.TypeUnion:
		return d.eval(in.DeunionIntoNameds())
	case *super.TypeError:
		val := d.eval(super.NewValue(typ.Type, in.Bytes()))
		return super.NewValue(d.sctx.LookupTypeError(val.Type()), val.Bytes())
	case *super.TypeFusion:
		out, errVal := d.downcast.defuse(typ, in.Bytes())
		if errVal != nil {
			return *errVal
		}
		return out
	default:
		// primitives, named types, enums
		// BTW, named types are a barrier to defuse.
		return in
	}
}

func (d *Defuse) parseArrayOrSet(typ super.Type, bytes scode.Bytes) []super.Value {
	var elems []super.Value
	for it := bytes.Iter(); !it.Done(); {
		elems = append(elems, d.eval(super.NewValue(typ, it.Next())))
	}
	return elems
}

func (d *Defuse) unify(elems []super.Value) (super.Type, scode.Bytes) {
	seen := make(map[super.Type]struct{})
	var types []super.Type
	for _, e := range elems {
		typ := e.Type()
		if _, ok := seen[typ]; !ok {
			seen[typ] = struct{}{}
			types = append(types, typ)
		}
	}
	if len(types) == 1 {
		var b scode.Builder
		for _, e := range elems {
			b.Append(e.Bytes())
		}
		return types[0], b.Bytes()
	}
	union := d.sctx.MustLookupTypeUnion(types)
	var b scode.Builder
	for _, e := range elems {
		super.BuildUnion(&b, union.TagOf(e.Type()), e.Bytes())
	}
	return union, b.Bytes()
}

func (d *Defuse) unifyType(vals []super.Value) super.Type {
	seen := make(map[super.Type]struct{})
	var types []super.Type
	for _, e := range vals {
		typ := e.Type()
		if _, ok := seen[typ]; !ok {
			seen[typ] = struct{}{}
			types = append(types, typ)
		}
	}
	switch len(types) {
	case 0:
		return super.TypeNull // XXX should be TypeNone
	case 1:
		return types[0]
	default:
		return d.sctx.MustLookupTypeUnion(types)
	}
}

type FusionChecker map[super.Type]bool

func (f *FusionChecker) Check(typ super.Type) bool {
	if *f == nil {
		*f = make(map[super.Type]bool)
	}
	return f.hasFusion(typ)
}

func (f FusionChecker) hasFusion(typ super.Type) bool {
	if fused, ok := f[typ]; ok {
		return fused
	}
	var has bool
	switch typ := typ.(type) {
	case *super.TypeRecord:
		has = slices.ContainsFunc(typ.Fields, func(field super.Field) bool { return f.hasFusion(field.Type) })
	case *super.TypeArray:
		has = f.hasFusion(typ.Type)
	case *super.TypeSet:
		has = f.hasFusion(typ.Type)
	case *super.TypeUnion:
		has = slices.ContainsFunc(typ.Types, f.hasFusion)
	case *super.TypeMap:
		has = f.hasFusion(typ.KeyType) || f.hasFusion(typ.ValType)
	case *super.TypeError:
		has = f.hasFusion(typ.Type)
	case *super.TypeOption:
		has = f.hasFusion(typ.Type)
	case *super.TypeFusion:
		has = true
	}
	f[typ] = has
	return has
}
