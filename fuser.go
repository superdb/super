package super

import (
	"fmt"
	"slices"
)

// Fuser constructs a fused supertype for all the types passed to Fuse.
type Fuser struct {
	sctx     *Context
	complete bool

	typ   Type
	types map[Type]struct{}
}

// XXX this is used by type checker but I think we can use the other one
func NewFuser(sctx *Context, complete bool) *Fuser {
	return &Fuser{sctx: sctx, complete: complete, types: make(map[Type]struct{})}
}

func (f *Fuser) Fuse(t Type) {
	if _, ok := f.types[t]; ok {
		return
	}
	f.types[t] = struct{}{}
	t = f.fuseInternal(t)
	if f.typ == nil {
		f.typ = t
	} else {
		f.typ = f.fuse(f.typ, t)
	}
}

// Type returns the computed supertype.
func (f *Fuser) Type() Type {
	return f.typ
}

func (f *Fuser) fuse(a, b Type) Type {
	if a == b {
		return a
	}
	if typ, ok := a.(*TypeFusion); ok {
		return f.fusion(f.fuse(typ.Type, b))
	}
	if typ, ok := b.(*TypeFusion); ok {
		return f.fusion(f.fuse(a, typ.Type))
	}
	if isAll(a) || isAll(b) {
		return TypeAll
	}
	switch a := a.(type) {
	case *TypeRecord:
		if b, ok := b.(*TypeRecord); ok {
			var fields []Field
			// Fuse fields in order they appear in the first record type a.
			// If a field is present in both a and b, the field types are fused.
			// If present in a but not b, the field is fused with type none.
			// This is how we recover the absence of a field vs a typed none in
			// an option-type field (aka optional field)
			for _, field := range a.Fields {
				var typ Type
				if k, ok := b.IndexOfField(field.Name); ok {
					typ = f.fuse(field.Type, b.Fields[k].Type)
				} else {
					typ = f.fuse(field.Type, TypeNone)

				}
				fields = append(fields, NewField(field.Name, typ))
			}
			// Now make sure any fields in b that are not in a are fused with
			// none and added to the end of the new record type.
			for _, field := range b.Fields {
				if a.HasField(field.Name) {
					continue
				}
				typ := f.fuse(field.Type, TypeNone)
				fields = append(fields, NewField(field.Name, typ))
			}
			fusedRec := f.sctx.MustLookupTypeRecord(fields)
			if !orderPreserved(fusedRec, a) || !orderPreserved(fusedRec, b) {
				return f.fusion(fusedRec)
			}
			return fusedRec
		}
	case *TypeArray:
		if b, ok := b.(*TypeArray); ok {
			return f.fusion(f.sctx.LookupTypeArray(f.fuse(a.Type, b.Type)))
		}
	case *TypeSet:
		if b, ok := b.(*TypeSet); ok {
			return f.fusion(f.sctx.LookupTypeSet(f.fuse(a.Type, b.Type)))
		}
	case *TypeMap:
		if b, ok := b.(*TypeMap); ok {
			keyType := f.fuse(a.KeyType, b.KeyType)
			valType := f.fuse(a.ValType, b.ValType)
			return f.fusion(f.sctx.LookupTypeMap(keyType, valType))
		}
	case *TypeUnion:
		types := f.fuseIntoUnionTypes(nil, a)
		types = f.fuseIntoUnionTypes(types, b)
		if len(types) == 1 {
			return types[0]
		}
		union := f.sctx.MustLookupTypeUnion(Flatten(types))
		return f.fusion(union)
	case *TypeEnum:
		if b, ok := b.(*TypeEnum); ok {
			var newSymbols []string
			for _, s := range b.Symbols {
				if !slices.Contains(a.Symbols, s) {
					newSymbols = append(newSymbols, s)
				}
			}
			if len(newSymbols) == 0 {
				return a
			}
			symbols := append(slices.Clone(a.Symbols), newSymbols...)
			return f.fusion(f.sctx.LookupTypeEnum(symbols))
		}
	case *TypeError:
		if b, ok := b.(*TypeError); ok {
			return f.fusion(f.sctx.LookupTypeError(f.fuse(a.Type, b.Type)))
		}
	case *TypeOption:
		if b, ok := b.(*TypeOption); ok {
			return f.fusion(f.sctx.LookupTypeOption(noFusion(f.fuse(a.Type, b.Type))))
		}
	case *TypeNamed:
		if b, ok := b.(*TypeNamed); ok && a.Name == b.Name {
			// if we got here without match a=b above, then there are
			// two different types with the same name, which the type
			// context shouldn't allow.
			f.redefPanic(a)
		}
		// We don't fuse the body of named types as they are unique and
		// a barrier to type fusion.  Instead we fall through here and ,
		// fuse the named type with the other type.
	}
	switch b.(type) {
	case *TypeUnion, *TypeOption:
		return f.fuse(b, a)
	}
	// Neither a nor b can be an anonymous union at this point.
	return f.fusion(f.sctx.MustLookupTypeUnion([]Type{a, b}))
}

func isAll(t Type) bool {
	_, ok := t.(*TypeOfAll)
	return ok
}

func (f *Fuser) redefPanic(named *TypeNamed) {
	previous := f.sctx.LookupByName(named.Name)
	panic(fmt.Sprintf("type %s redefined: %#v to %#v", named.Name, previous, named.Type))
}

func (f *Fuser) fuseInternal(typ Type) Type {
	if typ, ok := typ.(*TypeFusion); ok {
		return f.fusion(f.fuseInternal(typ.Type))
	}
	var out Type
	switch typ := typ.(type) {
	case *TypeRecord:
		fields := slices.Clone(typ.Fields)
		for i, field := range fields {
			fields[i].Type = f.fuseInternal(field.Type)
		}
		out = f.sctx.MustLookupTypeRecord(fields)
	case *TypeArray:
		out = f.sctx.LookupTypeArray(f.fuseInternal(typ.Type))
	case *TypeSet:
		out = f.sctx.LookupTypeSet(f.fuseInternal(typ.Type))
	case *TypeMap:
		out = f.sctx.LookupTypeMap(f.fuseInternal(typ.KeyType), f.fuseInternal(typ.ValType))
	case *TypeUnion:
		var types []Type
		for _, t := range typ.Types {
			types = f.fuseIntoUnionTypes(types, f.fuseInternal(t))
		}
		if len(types) == 1 {
			out = types[0]
		} else {
			out = f.sctx.MustLookupTypeUnion(Flatten(types))
		}
	case *TypeOption:
		out = f.sctx.LookupTypeOption(f.fuseInternal(typ.Type))
	case *TypeEnum:
		return typ
	case *TypeError:
		out = f.sctx.LookupTypeError(f.fuseInternal(typ.Type))
	default:
		out = typ
	}
	if out != typ {
		out = f.fusion(out)
	}
	return out
}

// fuseIntoUnionTypes fuses typ into types while maintaining the invariant that
// types contains at most one type of each complex kind but no unions.
func (f *Fuser) fuseIntoUnionTypes(types []Type, typ Type) []Type {
	switch typ := typ.(type) {
	case *TypeNamed:
		return f.addNamed(types, typ)
	case *TypeUnion:
		for _, t := range typ.Types {
			types = f.fuseIntoUnionTypes(types, t)
		}
		return types
	case *TypeFusion:
		return f.fuseIntoUnionTypes(types, typ.Type)
	}
	typKind := typ.Kind()
	for i, t := range types {
		switch {
		case t == typ:
			// This is already in the union.
			return types
		case typKind != PrimitiveKind && typKind == t.Kind() && !IsTypeNamed(t):
			types[i] = noFusion(f.fuse(t, typ))
			return types
		}
	}
	return append(types, noFusion(typ))
}

func (f *Fuser) addNamed(types []Type, named *TypeNamed) []Type {
	for _, t := range types {
		if existingNamed, ok := t.(*TypeNamed); ok && existingNamed.Name == named.Name {
			if existingNamed.Type != named.Type {
				f.redefPanic(named)
			}
			return types
		}
	}
	return append(types, named)
}

func noFusion(typ Type) Type {
	if s, ok := typ.(*TypeFusion); ok {
		return s.Type
	}
	return typ
}

func (f *Fuser) fusion(typ Type) Type {
	if !f.complete {
		return typ
	}
	if typ, ok := typ.(*TypeFusion); ok {
		return typ
	}
	return f.sctx.LookupTypeFusion(typ)
}

func orderPreserved(fused, child *TypeRecord) bool {
	off := -1
	for _, f := range child.Fields {
		if off < 0 {
			off, _ = fused.IndexOfField(f.Name)
			continue
		}
		next, _ := fused.IndexOfField(f.Name)
		if next < off {
			return false
		}
		off = next
	}
	return true
}
