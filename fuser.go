package super

import (
	"fmt"
	"slices"
	"strings"
)

// Fuser constructs a fused supertype for all the types passed to Fuse.
//
// The supertype is a type of the type system: it has no fusion types and
// any absorbs every type except missing (so the fusion of any with a type
// that may be missing is any|missing).  A fused type passed to Fuse is
// normalized to its supertype, with fusion(all) normalized to any.  The
// fused type (see FusedType) is a type of the data model, in which any
// becomes fusion(all).
//
// Fuse computes the supertype incrementally with the fields of each record
// type in alphabetical order.  SuperType then orders the fields of each
// record type by aggregating the field orders of the fused types (see
// orderFields) so that the result depends only on the set of fused types
// and not on the order in which they were fused.
type Fuser struct {
	sctx     *Context
	super    Type
	subtypes map[Type]struct{}
	// ordered caches super with ordered record fields.
	ordered Type
}

func NewFuser(sctx *Context) *Fuser {
	return &Fuser{sctx: sctx, subtypes: make(map[Type]struct{})}
}

func (f *Fuser) SuperType() Type {
	if f.ordered == nil && f.super != nil {
		f.ordered = f.orderFields()
	}
	return f.ordered
}

func (f *Fuser) FusedType() Type {
	fusion := f.SuperType()
	for sub := range f.subtypes {
		fusion = f.merge(fusion, sub)
	}
	// Any remaining any (e.g., when a fused type itself contained any) is
	// replaced since no value has type any.
	if fusion != nil {
		fusion = f.sctx.AnyToFusion(fusion)
	}
	return fusion
}

func (f *Fuser) Fuse(t Type) {
	if _, ok := f.subtypes[t]; ok {
		return
	}
	f.subtypes[t] = struct{}{}
	f.ordered = nil
	t = f.normalize(t)
	if f.super == nil {
		f.super = t
	} else {
		f.super = f.fuse(f.super, t)
	}
}

func (f *Fuser) fuse(a, b Type) Type {
	if a == b {
		return a
	}
	if typ, ok := a.(*TypeFusion); ok {
		return f.fuse(typ.Type, b)
	}
	if typ, ok := b.(*TypeFusion); ok {
		return f.fuse(a, typ.Type)
	}
	if isAny(a) || isAny(b) {
		return f.anyType(hasMissing(a) || hasMissing(b))
	}
	switch a := a.(type) {
	case *TypeRecord:
		if b, ok := b.(*TypeRecord); ok {
			var fields []Field
			for _, field := range a.Fields {
				var typ Type
				if k, ok := b.IndexOfField(field.Name); ok {
					typ = f.fuse(field.Type, b.Fields[k].Type)
				} else {
					typ = f.fuse(field.Type, TypeMissing)
				}
				fields = append(fields, NewField(field.Name, typ))
			}
			// Now make sure any fields in b that are not in a are fused with
			// none and added to the end of the new record type.
			for _, field := range b.Fields {
				if a.HasField(field.Name) {
					continue
				}
				typ := f.fuse(field.Type, TypeMissing)
				fields = append(fields, NewField(field.Name, typ))
			}
			slices.SortFunc(fields, func(x Field, y Field) int {
				return strings.Compare(x.Name, y.Name)
			})
			return f.sctx.MustLookupTypeRecord(fields)
		}
	case *TypeArray:
		if b, ok := b.(*TypeArray); ok {
			return f.sctx.LookupTypeArray(f.fuseElem(a.Type, b.Type))
		}
	case *TypeSet:
		if b, ok := b.(*TypeSet); ok {
			return f.sctx.LookupTypeSet(f.fuseElem(a.Type, b.Type))
		}
	case *TypeMap:
		if b, ok := b.(*TypeMap); ok {
			keyType := f.fuseElem(a.KeyType, b.KeyType)
			valType := f.fuseElem(a.ValType, b.ValType)
			return f.sctx.LookupTypeMap(keyType, valType)
		}
	case *TypeUnion:
		types := f.extendUnionTypes(nil, a, f.fuse)
		types = f.extendUnionTypes(types, b, f.fuse)
		return f.unionOf(types)
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
			return f.sctx.LookupTypeEnum(symbols)
		}
	case *TypeError:
		if b, ok := b.(*TypeError); ok {
			return f.sctx.LookupTypeError(f.fuse(a.Type, b.Type))
		}
	case *TypeOption:
		if b, ok := b.(*TypeOption); ok {
			return f.sctx.LookupTypeOption(f.fuse(a.Type, b.Type))
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
	return f.sctx.MustLookupTypeUnion([]Type{a, b})
}

// fuseElem fuses the element types of two containers.  The element type
// of an empty container is none, which has no values so is absorbed by
// the other element type.
func (f *Fuser) fuseElem(a, b Type) Type {
	if a == TypeNone {
		return b
	}
	if b == TypeNone {
		return a
	}
	return f.fuse(a, b)
}

// isAny returns true if t is any (or all, which is the any of a fused type
// that's been normalized).
func isAny(t Type) bool {
	switch t.(type) {
	case *TypeOfAny, *TypeOfAll:
		return true
	}
	return false
}

// hasMissing returns true if t is missing or a union containing missing.
func hasMissing(t Type) bool {
	if t == TypeMissing {
		return true
	}
	if union, ok := t.(*TypeUnion); ok {
		return slices.Contains(union.Types, Type(TypeMissing))
	}
	return false
}

// anyType returns any or, if missing is true, any|missing.
func (f *Fuser) anyType(missing bool) Type {
	if missing {
		return f.sctx.MustLookupTypeUnion([]Type{TypeAny, TypeMissing})
	}
	return TypeAny
}

// unionOf returns the type for the union types, which satisfy the invariant
// of extendUnionTypes, where any absorbs every type but missing.
func (f *Fuser) unionOf(types []Type) Type {
	if slices.ContainsFunc(types, isAny) {
		return f.anyType(slices.Contains(types, Type(TypeMissing)))
	}
	if len(types) == 1 {
		return types[0]
	}
	return f.sctx.MustLookupTypeUnion(Flatten(types))
}

func (f *Fuser) redefPanic(named *TypeNamed) {
	previous := f.sctx.LookupByName(named.Name)
	panic(fmt.Sprintf("type %s redefined: %#v to %#v", named.Name, previous, named.Type))
}

func (f *Fuser) normalize(typ Type) Type {
	if typ, ok := typ.(*TypeFusion); ok {
		// fusion(all) normalizes to all, which is any in the supertype.
		return f.normalize(typ.Type)
	}
	if isAny(typ) {
		return TypeAny
	}
	switch typ := typ.(type) {
	case *TypeRecord:
		fields := slices.Clone(typ.Fields)
		for i, field := range fields {
			fields[i].Type = f.normalize(field.Type)
		}
		return f.sctx.MustLookupTypeRecord(fields)
	case *TypeArray:
		return f.sctx.LookupTypeArray(f.normalize(typ.Type))
	case *TypeSet:
		return f.sctx.LookupTypeSet(f.normalize(typ.Type))
	case *TypeMap:
		return f.sctx.LookupTypeMap(f.normalize(typ.KeyType), f.normalize(typ.ValType))
	case *TypeUnion:
		var types []Type
		for _, t := range typ.Types {
			types = f.extendUnionTypes(types, f.normalize(t), f.fuse)
		}
		return f.unionOf(types)
	case *TypeOption:
		return f.sctx.LookupTypeOption(f.normalize(typ.Type))
	case *TypeError:
		return f.sctx.LookupTypeError(f.normalize(typ.Type))
	default:
		return typ
	}
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
	if typ, ok := typ.(*TypeFusion); ok {
		return typ
	}
	return f.sctx.MustLookupTypeFusion(typ)
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

func (f *Fuser) merge(super, sub Type) Type {
	if isAny(super) || unionHasAny(super) {
		// A value fused into any (or a union containing any) is a value
		// of fusion(all), which carries the value's own type.
		return f.sctx.FusionAll()
	}
	if super == sub {
		return super
	}
	if super, ok := super.(*TypeFusion); ok {
		return f.fusion(f.merge(super.Type, sub))
	}
	if sub, ok := sub.(*TypeFusion); ok {
		return f.fusion(f.merge(super, sub.Type))
	}
	if union, ok := sub.(*TypeUnion); ok {
		if _, ok := super.(*TypeUnion); !ok {
			// The members of sub were fused into the single type super
			// (e.g., a union of record types fused into one record type).
			// Merge each member and mark super as a fusion since sub's
			// values are union values.
			for _, t := range union.Types {
				super = noFusion(f.merge(super, t))
			}
			return f.fusion(super)
		}
	}
	switch super := super.(type) {
	case *TypeRecord:
		sub := sub.(*TypeRecord)
		superFields := slices.Clone(super.Fields)
		for _, subField := range sub.Fields {
			col, ok := super.IndexOfField(subField.Name)
			if !ok {
				panic(subField.Name)
			}
			superFields[col].Type = f.merge(superFields[col].Type, subField.Type)
		}
		// A field of super that's not in sub is missing in sub's values.
		for k, superField := range super.Fields {
			if !sub.HasField(superField.Name) {
				superFields[k].Type = f.merge(superFields[k].Type, TypeMissing)
			}
		}
		superType := f.sctx.MustLookupTypeRecord(superFields)
		// The fused fields record their subtypes (including missing for
		// a field absent from sub) so the record itself is a fusion only
		// when sub's fields are in a different order.
		if !orderPreserved(super, sub) {
			return f.fusion(superType)
		}
		return superType
	case *TypeArray:
		sub := sub.(*TypeArray)
		// always fuse array because when we get here, super != sub
		return f.fusion(f.sctx.LookupTypeArray(f.mergeElem(super.Type, sub.Type)))
	case *TypeSet:
		sub := sub.(*TypeSet)
		return f.fusion(f.sctx.LookupTypeSet(f.mergeElem(super.Type, sub.Type)))
	case *TypeMap:
		sub := sub.(*TypeMap)
		keyType := f.mergeElem(super.KeyType, sub.KeyType)
		valType := f.mergeElem(super.ValType, sub.ValType)
		return f.fusion(f.sctx.LookupTypeMap(keyType, valType))
	case *TypeUnion:
		// Clone since extendUnionTypes modifies types and super.Types
		// belongs to the type context's union type.
		types := f.extendUnionTypes(slices.Clone(super.Types), sub, f.merge)
		if len(types) == 1 {
			return types[0]
		}
		union := f.sctx.MustLookupTypeUnion(Flatten(types))
		return f.fusion(union)
	case *TypeEnum:
		sub := sub.(*TypeEnum)
		var newSymbols []string
		for _, s := range sub.Symbols {
			if !slices.Contains(super.Symbols, s) {
				newSymbols = append(newSymbols, s)
			}
		}
		if len(newSymbols) == 0 {
			return super
		}
		symbols := append(slices.Clone(super.Symbols), newSymbols...)
		return f.fusion(f.sctx.LookupTypeEnum(symbols))
	case *TypeError:
		sub := sub.(*TypeError)
		return f.fusion(f.sctx.LookupTypeError(f.merge(super.Type, sub.Type)))
	case *TypeOption:
		sub := sub.(*TypeOption)
		return f.fusion(f.sctx.LookupTypeOption(noFusion(f.merge(super.Type, sub.Type))))
	case *TypeNamed:
		sub := sub.(*TypeNamed)
		if super.Name == sub.Name {
			// if we got here without match a=b above, then there are
			// two different types with the same name, which the type
			// context shouldn't allow.
			f.redefPanic(super)
		}
		// We don't fuse the body of named types as they are unique and
		// a barrier to type fusion.
		return super
	default:
		panic(super)
	}
}

// unionHasAny returns true if t is a union with any as a member.
func unionHasAny(t Type) bool {
	union, ok := t.(*TypeUnion)
	return ok && slices.ContainsFunc(union.Types, isAny)
}

func (f *Fuser) mergeElem(a, b Type) Type {
	if a == TypeNone {
		return b
	}
	if b == TypeNone {
		return a
	}
	return f.merge(a, b)
}

// extendUnionTypes adds typ to types while maintaining the invariant that
// types contains at most one type of each complex kind and no unions.
// A type of the same complex kind as an existing member is combined with
// it using combine (fuse when computing the supertype, merge when
// computing the fused type).  The types slice may be mutated.
func (f *Fuser) extendUnionTypes(types []Type, typ Type, combine func(a, b Type) Type) []Type {
	switch typ := typ.(type) {
	case *TypeNamed:
		return f.addNamed(types, typ)
	case *TypeUnion:
		for _, t := range typ.Types {
			types = f.extendUnionTypes(types, t, combine)
		}
		return types
	case *TypeFusion:
		return f.extendUnionTypes(types, typ.Type, combine)
	}
	kind := typ.Kind()
	for i, t := range types {
		switch {
		case t == typ:
			return types
		case kind != PrimitiveKind && kind == t.Kind() && !IsTypeNamed(t):
			types[i] = noFusion(combine(t, typ))
			return types
		}
	}
	return append(types, noFusion(typ))
}
