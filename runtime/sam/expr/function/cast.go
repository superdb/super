package function

import (
	"maps"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sup"
)

type Caster interface {
	Cast(from super.Value, to super.Type) (super.Value, bool)
}

type cast struct {
	sctx   *super.Context
	upcast *Upcast
}

func NewCaster(sctx *super.Context) Caster {
	return &cast{sctx: sctx}
}

func (c *cast) Call(args []super.Value) super.Value {
	from, to := args[0], args[1]
	from = from.SuperDeunion()
	if from.IsError() {
		return from
	}
	switch toUnder := to.Under(); toUnder.Type().ID() {
	case super.IDString:
		typ, err := c.sctx.LookupTypeNamed(toUnder.AsString(), super.TypeUnder(from.Type()))
		if err != nil {
			return c.sctx.NewError(err)
		}
		return super.NewValue(typ, from.Bytes())
	case super.IDType:
		typ, err := c.sctx.LookupByValue(toUnder.Bytes())
		if err != nil {
			panic(err)
		}
		val, _ := c.Cast(from, typ)
		return val
	}
	return c.sctx.WrapError("cast target must be a type or type name", to)
}

func (c *cast) Cast(from super.Value, to super.Type) (super.Value, bool) {
	if optionType, ok := to.(*super.TypeOption); ok {
		if from.IsNone() {
			return optionType.None(), true
		}
		val, ok := c.Cast(from.Unwrap(), optionType.Type)
		if !ok {
			return val, ok
		}
		return optionType.Some(val.Bytes()), true
	}
	from = from.DeunionIntoNameds()

	switch fromType := from.Type(); {
	case fromType == to:
		return from, true
	case fromType.ID() == to.ID():
		return super.NewValue(to, from.Bytes()), true
	case fromType == super.TypeNull:
		union := c.sctx.Nullable(to)
		var b scode.Builder
		super.BuildUnion(&b, union.TagOf(super.TypeNull), nil)
		return super.NewValue(union, b.Bytes().Body()), true
	case fromType == super.TypeMissing:
		// Casting a none::missing value to a non option type.  Automatically convert
		// the target type to it's option form.
		if _, ok := super.TypeUnder(to).(*super.TypeUnion); !ok {
			// As in SUP, a cast applied to an untyped none implieds an
			// option type, except for unions, which casts the none to
			// a member of the union.  If you want to cast to an option union,
			// you need to be explicit and specify the option type.  So
			// here we check that this is not a union and fall through below
			// if it is.
			return c.sctx.LookupTypeOption(to).None(), true
		}
	}
	switch to := to.(type) {
	case *super.TypeRecord:
		return c.toRecord(from, to)
	case *super.TypeArray, *super.TypeSet:
		return c.toArrayOrSet(from, to)
	case *super.TypeMap:
		return c.toMap(from, to)
	case *super.TypeUnion:
		return c.toUnion(from, to)
	case *super.TypeError:
		return c.toError(from, to)
	case *super.TypeNamed:
		return c.toNamed(from, to)
	case *super.TypeFusion:
		if to.Type != super.TypeAll {
			return c.error(from, to)
		}
		if c.upcast == nil {
			c.upcast = NewUpcast(c.sctx)
		}
		val, ok := c.upcast.Cast(from, to)
		if !ok {
			return c.error(from, to)
		}
		return val, true
	default:
		caster := expr.LookupPrimitiveCaster(c.sctx, to)
		if caster == nil {
			return c.error(from, to)
		}
		val := caster.Eval(from)
		return val, !val.IsError()
	}
}

func (c *cast) error(from super.Value, to super.Type) (super.Value, bool) {
	return c.sctx.WrapError("cannot cast to "+sup.FormatType(to), from), false
}

func (c *cast) toRecord(from super.Value, to *super.TypeRecord) (super.Value, bool) {
	fromRecType := super.TypeRecordOf(from.Type())
	if fromRecType == nil {
		return c.error(from, to)
	}
	var b scode.Builder
	var fields []super.Field
	b.BeginContainer()
	aok := true
	for i, f := range to.Fields {
		var val2 super.Value
		fieldVal := from.Deref(f.Name) // deref(fromRecType, from.Bytes(), f.Name)
		if fieldVal.IsMissing() {
			// This field isn't present.  If the target type is optional,
			// code a none value.  Otherwise, code error missing.
			if optionType, ok := f.Type.(*super.TypeOption); ok {
				val2 = optionType.None()
			} else {
				val2 = c.sctx.Missing()
			}
		} else {
			var ok bool
			val2, ok = c.Cast(*fieldVal, f.Type)
			aok = aok && ok
		}
		if t := val2.Type(); t != f.Type {
			if fields == nil {
				fields = slices.Clone(to.Fields)
			}
			fields[i].Type = t
		}
		b.Append(val2.Bytes())
	}
	if fields != nil {
		to = c.sctx.MustLookupTypeRecord(fields)
	}
	b.EndContainer()
	return super.NewValue(to, b.Bytes().Body()), aok
}

func (c *cast) toArrayOrSet(from super.Value, to super.Type) (super.Value, bool) {
	fromInner := super.InnerType(from.Type())
	toInner := super.InnerType(to)
	if fromInner == nil {
		// XXX Should also return an error if casting from fromInner to
		// toInner will always fail.
		return c.error(from, to)
	}
	types := map[super.Type]struct{}{}
	var vals []super.Value
	aok := true
	for it := from.ContainerIter(); !it.Done(); {
		val, ok := c.castNext(&it, fromInner, toInner)
		aok = aok && ok
		types[val.Type()] = struct{}{}
		vals = append(vals, val)
	}
	if len(vals) == 0 {
		return super.NewValue(to, from.Bytes()), aok
	}
	inner, ok := c.maybeConvertToUnion(vals, types)
	aok = aok && ok
	if inner != toInner {
		if to.Kind() == super.ArrayKind {
			to = c.sctx.LookupTypeArray(inner)
		} else {
			to = c.sctx.LookupTypeSet(inner)
		}
	}
	var bytes scode.Bytes
	for _, val := range vals {
		bytes = scode.Append(bytes, val.Bytes())
	}
	if to.Kind() == super.SetKind {
		bytes = super.NormalizeSet(bytes)
	}
	return super.NewValue(to, bytes), aok
}

func (c *cast) castNext(it *scode.Iter, from, to super.Type) (super.Value, bool) {
	val := super.NewValue(from, it.Next())
	return c.Cast(val, to)
}

func (c *cast) maybeConvertToUnion(vals []super.Value, types map[super.Type]struct{}) (super.Type, bool) {
	typesSlice := super.Flatten(slices.Collect(maps.Keys(types)))
	if len(typesSlice) == 1 {
		return typesSlice[0], true
	}
	union := c.sctx.MustLookupTypeUnion(typesSlice)
	aok := true
	for i, val := range vals {
		var ok bool
		vals[i], ok = c.toUnion(val, union)
		aok = aok && ok
	}
	return union, aok
}

func (c *cast) toMap(from super.Value, to *super.TypeMap) (super.Value, bool) {
	fromType, ok := from.Type().(*super.TypeMap)
	if !ok {
		return c.error(from, to)
	}
	keyTypes := map[super.Type]struct{}{}
	valTypes := map[super.Type]struct{}{}
	var keyVals, valVals []super.Value
	aok := true
	for it := from.ContainerIter(); !it.Done(); {
		keyVal, ok := c.castNext(&it, fromType.KeyType, to.KeyType)
		aok = aok && ok
		keyVals = append(keyVals, keyVal)
		keyTypes[keyVal.Type()] = struct{}{}
		valVal, ok := c.castNext(&it, fromType.ValType, to.ValType)
		aok = aok && ok
		valTypes[valVal.Type()] = struct{}{}
		valVals = append(valVals, valVal)
	}
	if len(keyVals) == 0 {
		return super.NewValue(to, from.Bytes()), aok
	}
	keyType, ok := c.maybeConvertToUnion(keyVals, keyTypes)
	aok = aok && ok
	valType, ok := c.maybeConvertToUnion(valVals, valTypes)
	aok = aok && ok
	if keyType != to.KeyType || valType != to.ValType {
		to = c.sctx.LookupTypeMap(keyType, valType)
	}
	var bytes scode.Bytes
	for i := range keyVals {
		bytes = scode.Append(bytes, keyVals[i].Bytes())
		bytes = scode.Append(bytes, valVals[i].Bytes())
	}
	return super.NewValue(to, super.NormalizeMap(bytes)), aok
}

func (c *cast) toUnion(from super.Value, to *super.TypeUnion) (super.Value, bool) {
	tag := bestUnionTag(from.Type(), to)
	if tag < 0 {
		from2 := from.DeunionIntoNameds()
		tag = bestUnionTag(from2.Type(), to)
		if tag < 0 {
			// This is here for infer which generates single-type union missings
			// of the form (T|missing).  It's not a general solution to which
			// type of the union is a best option cast when there's not an exact
			// match.  Reasonable semantics might be to pick a random one, but
			// for now, we just handle single-element union missings.
			if inner, ok := singletonUnionMissing(to.Types); ok {
				val, ok := c.Cast(from2, inner)
				if !ok {
					return val, false
				}
				return c.toUnion(val, to)
			}
			return c.error(from, to)
		}
		from = from2
	}
	var b scode.Builder
	super.BuildUnion(&b, tag, from.Bytes())
	return super.NewValue(to, b.Bytes().Body()), true
}

func singletonUnionMissing(types []super.Type) (super.Type, bool) {
	if len(types) == 2 {
		if types[0] == super.TypeMissing {
			return types[1], true
		}
		if types[1] == super.TypeMissing {
			return types[0], true
		}
	}
	return nil, false
}

func (c *cast) toError(from super.Value, to *super.TypeError) (super.Value, bool) {
	from, ok := c.Cast(from, to.Type)
	if from.Type() != to.Type {
		return from, ok
	}
	return super.NewValue(to, from.Bytes()), ok
}

func (c *cast) toNamed(from super.Value, to *super.TypeNamed) (super.Value, bool) {
	from, ok := c.Cast(from, to.Type)
	if from.Type() != to.Type {
		return from, ok
	}
	return super.NewValue(to, from.Bytes()), ok
}

// bestUnionTag tries to return the most specific union tag for in
// within out.  It returns -1 if out is not a union or contains no type
// compatible with in.  (Types are compatible if they have the same underlying
// type.)  If out contains in, BestUnionTag returns its tag.
// Otherwise, if out contains in's underlying type, BestUnionTag returns
// its tag.  Finally, BestUnionTag returns the smallest tag in
// out whose type is compatible with in.
func bestUnionTag(in, out super.Type) int {
	outUnion, ok := super.TypeUnder(out).(*super.TypeUnion)
	if !ok {
		return -1
	}
	typeUnderIn := super.TypeUnder(in)
	underlying := -1
	compatible := -1
	for i, t := range outUnion.Types {
		if t == in {
			return i
		}
		if t == typeUnderIn && underlying == -1 {
			underlying = i
		}
		if super.TypeUnder(t) == typeUnderIn && compatible == -1 {
			compatible = i
		}
	}
	if underlying != -1 {
		return underlying
	}
	return compatible
}
