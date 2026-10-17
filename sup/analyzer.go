package sup

import (
	"errors"
	"fmt"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/ast"
	"github.com/superdb/super/scode"
)

type Value interface {
	Type() super.Type
}

// Note that all of the types include a generic super.Type as their type since
// anything can have a super.TypeNamed along with its normal type.
type (
	Primitive struct {
		typ  super.Type
		text string
	}
	Record struct {
		typ    super.Type
		fields []Value
	}
	Array struct {
		typ   super.Type
		elems []Value
	}
	Set struct {
		typ   super.Type
		elems []Value
	}
	Union struct {
		typ   super.Type
		tag   int
		value Value
	}
	Enum struct {
		typ  super.Type
		name string
	}
	Map struct {
		typ     super.Type
		entries []Entry
	}
	Entry struct {
		key   Value
		value Value
	}
	Null struct {
		// Nulls aren't typed but they can be named so this field holds
		// the named type when decorated as such.
		typ super.Type
	}
	TypeValue struct {
		typ   super.Type
		value super.Type
	}
	Error struct {
		typ   super.Type
		value Value
	}
	Fusion struct {
		typ     super.Type
		value   Value
		subtype scode.Bytes
	}
	None struct {
		// Nones are either missing value (type missing) or option types.
		// This struct serves as a placeoholder until they can be decorated
		// into an option type, or it may remain a none::missing.
		typ super.Type
	}
	Option struct {
		typ   super.Type
		value Value
	}
)

func NewPrimitive(typ super.Type, text string) Primitive {
	return Primitive{typ, text}
}

func (p *Primitive) Type() super.Type { return p.typ }
func (r *Record) Type() super.Type    { return r.typ }
func (a *Array) Type() super.Type     { return a.typ }
func (s *Set) Type() super.Type       { return s.typ }
func (u *Union) Type() super.Type     { return u.typ }
func (e *Enum) Type() super.Type      { return e.typ }
func (m *Map) Type() super.Type       { return m.typ }
func (n *Null) Type() super.Type      { return n.typ }
func (t *TypeValue) Type() super.Type { return t.typ }
func (e *Error) Type() super.Type     { return e.typ }
func (f *Fusion) Type() super.Type    { return f.typ }
func (n *None) Type() super.Type      { return n.typ }
func (o *Option) Type() super.Type    { return o.typ }

type Analyzer struct {
	sctx *super.Context
}

func NewAnalyzer(sctx *super.Context) *Analyzer {
	return &Analyzer{
		sctx: sctx,
	}
}

func (a *Analyzer) ConvertValue(val ast.Value) (Value, error) {
	return a.convertValue(val)
}

// BindTypeScope provides a means to create named types from declarations.
// This is used by the semantic analyzer to create persistent typedefs for
// each named type defined in the query (which contrasts from named types
// that are defined by the data).  All such bindings are stored in the typedefs
// table local to this Analyzer relative to the type IDs that are returned here.
// This typedefs table plus the IDs can then be used to serialize all the
// types defined by the query as a DAG.  At query runtime, the DAG typedefs
// are translated to query context types and the runtime references by typeID
// are resolved to actual super.Types relative to query context.
// The decls arguments must have unique names.
func (a *Analyzer) BindTypeScope(decls []*ast.TypeDecl) ([]int, []error) {
	patches := make([]int, len(decls))
	nameds := make([]*super.TypeNamed, len(decls))
	errors := make([]error, len(decls))
	var haveErr bool
	for k, d := range decls {
		named, patch, err := a.sctx.DeclareTypeNamed(d.Name.Name)
		if err != nil {
			haveErr = true
			errors[k] = err
		}
		patches[k] = patch
		nameds[k] = named
	}
	var ids []int
	for k, d := range decls {
		if errors[k] == nil {
			inner, err := a.convertType(d.Type)
			if err != nil {
				errors[k] = err
				haveErr = true
				// Avoid deadlock waiting for declared-but-not-resolved type
				inner = super.TypeMissing
			}
			if err := a.sctx.BindTypeNamed(patches[k], nameds[k], inner); err != nil {
				errors[k] = err
				haveErr = true
			}
			ids = append(ids, super.TypeID(nameds[k]))
		}
	}
	if haveErr {
		return nil, errors
	}
	return ids, nil
}

func (a *Analyzer) LookupType(t ast.Type) (int, error) {
	typ, err := a.convertType(t)
	if err != nil {
		return 0, err
	}
	return super.TypeID(typ), nil
}

func (a *Analyzer) convertValueAndDecorate(val ast.Value, decorator super.Type) (Value, error) {
	v, err := a.convertValue(val)
	if err != nil {
		return nil, err
	}
	return a.decorate(v, decorator)
}

func (a *Analyzer) convertValue(val ast.Value) (Value, error) {
	switch val := val.(type) {
	case *ast.DeclsValue:
		if err := a.bindTypeDecls(val.Decls); err != nil {
			return nil, err
		}
		v, err := a.convertValue(val.Value)
		if err != nil {
			return nil, err
		}
		return v, nil
	case *ast.Decorated:
		if val.Type != nil {
			decorator, err := a.convertType(val.Type)
			if err != nil {
				return nil, err
			}
			return a.convertValueAndDecorate(val.Value, decorator)
		}
		return a.convertValue(val.Value)
	case *ast.None:
		if val.Type != nil {
			typ, err := a.convertType(val.Type)
			if err != nil {
				return nil, err
			}
			return &None{typ}, nil
		}
		return &None{typ: super.TypeMissing}, nil
	case *ast.Primitive:
		return a.convertPrimitive(val)
	case *ast.TypeValue:
		return a.convertTypeValue(val)
	case *ast.Record:
		return a.convertRecord(val)
	case *ast.Array:
		return a.convertArray(val)
	case *ast.Set:
		return a.convertSet(val)
	case *ast.Map:
		return a.convertMap(val)
	case *ast.Error:
		return a.convertError(val)
	case *ast.Fusion:
		return a.convertFusion(val)
	case *ast.Some:
		return a.convertSome(val)
	default:
		panic(val)
	}
}

func (a *Analyzer) bindTypeDecls(decls []ast.TypeDecl) error {
	var patches []int
	var nameds []*super.TypeNamed
	for _, decl := range decls {
		named, patch, err := a.sctx.DeclareTypeNamed(decl.Name.Name)
		if err != nil {
			return err
		}
		patches = append(patches, patch)
		nameds = append(nameds, named)
	}
	// Take two passes over the decls.  The first pass converts the
	// types that this thread is responsible for binding and binds them.
	// The second pass converts types that we are not responsible for
	// binding and checks that the type bound elsewhere is the same type
	// as this declaration.
	var bindingErr error
	for k, decl := range decls {
		if patch := patches[k]; patch >= 0 {
			typ, err := a.convertType(decl.Type)
			if err != nil {
				typ = super.TypeMissing
				bindingErr = err
			}
			if err := a.sctx.BindTypeNamed(patch, nameds[k], typ); err != nil {
				bindingErr = err
			}
		}
	}
	if bindingErr != nil {
		return bindingErr
	}
	for k, decl := range decls {
		if patch := patches[k]; patch < 0 {
			typ, err := a.convertType(decl.Type)
			if err != nil {
				return err
			}
			// A different concurrent thread took responsibility for
			// binding this named type.  Check that that declaration
			// is the same as the one here.
			if named := nameds[k]; named.Type != typ {
				// XXX type conflicts are currently a fatal errors.
				// In a future version of SUP, we will have an option
				// to continue parsing data with such errors and create
				// structured errors for each named type with a conclict.
				return fmt.Errorf("type %q redefined", named.Name)
			}
		}
	}
	return nil
}

func (a *Analyzer) convertPrimitive(val *ast.Primitive) (Value, error) {
	typ := super.LookupPrimitive(val.Type)
	if typ == nil {
		return nil, fmt.Errorf("no such primitive type: %q", val.Type)
	}
	// Null's are here to possibly be decorated with a named type.
	if typ == super.TypeNull {
		return &Null{typ: super.TypeNull}, nil
	}
	return &Primitive{typ: typ, text: val.Text}, nil
}

func (a *Analyzer) convertTypeValue(tv *ast.TypeValue) (Value, error) {
	typ, err := a.convertType(tv.Value)
	if err != nil {
		return nil, err
	}
	return &TypeValue{
		typ:   super.TypeType,
		value: typ,
	}, nil
}

func (a *Analyzer) convertRecord(val *ast.Record) (Value, error) {
	vals := make([]Value, 0, len(val.Fields))
	fields := make([]super.Field, 0, len(val.Fields))
	for _, f := range val.Fields {
		val, err := a.convertValue(f.Value)
		if err != nil {
			return nil, err
		}
		typ := val.Type()
		if f.Opt {
			// Wrap type in an option type if it's not already expressed as such.
			// Note that we leave a missing as is and detect this condition
			// later so that decoration can turn it into an option none.  This can be
			// especially useful for named types.
			if !super.IsOptionType(typ) && super.TypeUnder(typ) != super.TypeMissing {
				typ = a.sctx.LookupTypeOption(typ)
				val = &Option{
					typ:   typ,
					value: val,
				}
			}
		}
		fields = append(fields, super.NewField(f.Name, typ))
		vals = append(vals, val)
	}
	return &Record{
		typ:    a.sctx.MustLookupTypeRecord(fields),
		fields: vals,
	}, nil
}

func (a *Analyzer) convertArray(array *ast.Array) (Value, error) {
	elems := make([]Value, 0, len(array.Elements))
	for _, elem := range array.Elements {
		v, err := a.convertValue(elem)
		if err != nil {
			return nil, err
		}
		elems = append(elems, v)
	}
	elems, elemType, err := a.normalizeElems(elems)
	if err != nil {
		return nil, err
	}
	return &Array{
		typ:   a.sctx.LookupTypeArray(elemType),
		elems: elems,
	}, nil
}

func (a *Analyzer) convertSet(set *ast.Set) (Value, error) {
	elems := make([]Value, 0, len(set.Elements))
	for _, elem := range set.Elements {
		v, err := a.convertValue(elem)
		if err != nil {
			return nil, err
		}
		elems = append(elems, v)
	}
	elems, elemType, err := a.normalizeElems(elems)
	if err != nil {
		return nil, err
	}
	return &Set{
		typ:   a.sctx.LookupTypeSet(elemType),
		elems: elems,
	}, nil
}

func (a *Analyzer) convertMap(m *ast.Map) (Value, error) {
	var keyType, valType super.Type
	keys := make([]Value, 0, len(m.Entries))
	vals := make([]Value, 0, len(m.Entries))
	for _, e := range m.Entries {
		key, err := a.convertValue(e.Key)
		if err != nil {
			return nil, err
		}
		val, err := a.convertValue(e.Value)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
		vals = append(vals, val)
	}
	var err error
	keys, keyType, err = a.normalizeElems(keys)
	if err != nil {
		return nil, err
	}
	vals, valType, err = a.normalizeElems(vals)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(keys))
	for i := range keys {
		entries = append(entries, Entry{keys[i], vals[i]})
	}
	return &Map{
		typ:     a.sctx.LookupTypeMap(keyType, valType),
		entries: entries,
	}, nil
}

func (a *Analyzer) normalizeElems(vals []Value) ([]Value, super.Type, error) {
	if len(vals) == 0 {
		return nil, super.TypeMissing, nil
	}
	types := make([]super.Type, len(vals))
	for i, val := range vals {
		types[i] = val.Type()
	}
	unique := super.UniqueTypes(super.Flatten(types))
	if len(unique) == 1 {
		return vals, unique[0], nil
	}
	if len(unique) == 0 {
		return vals, super.TypeMissing, nil
	}
	union, ok := a.sctx.LookupTypeUnion(unique)
	if !ok {
		return nil, nil, errAnonUnion
	}
	var unions []Value
	for _, v := range vals {
		val, err := a.decorate(v, union)
		if err != nil {
			return nil, nil, err
		}
		unions = append(unions, val)
	}
	return unions, union, nil
}

func (a Analyzer) convertError(val *ast.Error) (Value, error) {
	v, err := a.convertValue(val.Value)
	if err != nil {
		return nil, err
	}
	return &Error{
		typ:   a.sctx.LookupTypeError(v.Type()),
		value: v,
	}, nil
}

func (a Analyzer) convertFusion(val *ast.Fusion) (Value, error) {
	superVal, err := a.convertValue(val.Value)
	if err != nil {
		return nil, err
	}
	subType, err := a.convertTypeValue(val.Type)
	if err != nil {
		return nil, err
	}
	typ, err := a.sctx.LookupTypeFusion(superVal.Type())
	if err != nil {
		return nil, err
	}
	return &Fusion{
		value:   superVal,
		typ:     typ,
		subtype: a.sctx.LookupTypeValue(subType.(*TypeValue).value).Bytes(),
	}, nil
}

func (a Analyzer) convertSome(val *ast.Some) (Value, error) {
	v, err := a.convertValue(val.Value)
	if err != nil {
		return nil, err
	}
	if v.Type() == super.TypeMissing {
		return nil, errors.New("some() cannot contain a missing value")
	}
	if super.IsOptionType(v.Type()) {
		return nil, errors.New("some() cannot contain an option value")
	}
	return &Option{
		typ:   a.sctx.LookupTypeOption(v.Type()),
		value: v,
	}, nil
}

func (a *Analyzer) decorate(val Value, typ super.Type) (Value, error) {
	if val.Type() == typ {
		return val, nil
	}
	if _, ok := super.TypeUnder(typ).(*super.TypeUnion); ok {
		return a.createUnion(val, typ)
	}
	if super.IsTypeAny(typ) {
		return a.createAny(val), nil
	}
	if optionType, ok := super.TypeUnder(typ).(*super.TypeOption); ok {
		if option, ok := val.(*Option); ok {
			inner, err := a.decorate(option.value, optionType.Type)
			if err != nil {
				return nil, err
			}
			return &Option{typ: typ, value: inner}, nil
		}
		if none, ok := val.(*None); ok {
			// Untyped nones are allowed into the value structure on the first
			// pass then decoration can turn the plain none into an option none.
			// For example, this allows named types to decorate a complex record
			// without having to specify the decorator on all the embedded nones.
			return &Option{typ: typ, value: none}, nil
		}
		inner, err := a.decorate(val, optionType.Type)
		if err != nil {
			return nil, err
		}
		return &Option{typ: typ, value: inner}, nil
	}
	switch val := val.(type) {
	case *None:
		// Decorating a none turns it into a typed option none, unless it
		// is a named-type missing (or redundantly decorated none::missing),
		// which stays a none.
		if super.TypeUnder(typ) == super.TypeMissing {
			return &None{typ}, nil
		}
		return &Option{value: val, typ: a.sctx.LookupTypeOption(typ)}, nil
	case *Null:
		if super.TypeUnder(typ) != super.TypeNull {
			return nil, fmt.Errorf("illegal null value decorator: %q", FormatType(typ))
		}
		return &Null{typ: typ}, nil
	case *Primitive:
		return a.decoratePrimitive(val, typ)
	case *Record:
		return a.decorateRecord(val, typ)
	case *Array:
		return a.decorateArray(val, typ)
	case *Set:
		return a.decorateSet(val, typ)
	case *Map:
		return a.decorateMap(val, typ)
	case *TypeValue:
		return a.decorateTypeValue(val, typ)
	case *Error:
		return a.decorateError(val, typ)
	case *Fusion:
		return nil, fmt.Errorf("fusion values cannot be decorated: %q", FormatType(typ))
	case *Union:
		return a.decorateUnion(val, typ)
	case *Option:
		if _, ok := val.value.(*None); !ok {
			// typ cannot be an option type as that case is handled above
			return nil, errors.New("cannot decorate an option value with a non-option type")
		}
		return &Option{typ: a.sctx.Optionize(typ), value: val.value}, nil
	default:
		panic(val)
	}
}

func (a *Analyzer) createUnion(val Value, decorator super.Type) (Value, error) {
	unionType := super.TypeUnder(decorator).(*super.TypeUnion)
	typ := val.Type()
	if typ == decorator {
		return val, nil
	}
	if union, ok := val.(*Union); ok {
		if !super.IsTypeNamed(val.Type()) {
			// If we're putting an anonymous union inside of another union then we
			// need to unflatten the union relationship by deunioning the value,
			// which can then be inserted into the flat parent.  If this is a named
			// union, we do not do so as named unions in unions need not be flattened.
			val = union.value
			typ = val.Type()
		}
	}
	for k, t := range unionType.Types {
		if typ == t {
			return &Union{
				typ:   decorator,
				tag:   k,
				value: val,
			}, nil
		}
	}
	return nil, fmt.Errorf("%q is not in union type %q", FormatType(typ), FormatType(unionType))
}

func (a *Analyzer) createAny(val Value) Value {
	if super.IsTypeAny(val.Type()) {
		return val
	}
	typ := a.sctx.MustLookupTypeFusion(super.TypeAll)
	return &Fusion{
		value:   val,
		typ:     typ,
		subtype: a.sctx.LookupTypeValue(val.Type()).Bytes(),
	}
}

func (a *Analyzer) decoratePrimitive(val *Primitive, decorator super.Type) (Value, error) {
	if enumType, ok := super.TypeUnder(decorator).(*super.TypeEnum); ok {
		return a.decorateEnum(val, enumType, decorator)
	}
	if optionType, ok := super.TypeUnder(decorator).(*super.TypeOption); ok {
		return &Option{
			typ:   optionType,
			value: val,
		}, nil
	}
	if err := primitiveOk(val.typ, decorator); err != nil {
		return nil, err
	}
	return &Primitive{typ: decorator, text: val.text}, nil
}

func primitiveOk(typ, decorator super.Type) error {
	typID, castID := typ.ID(), decorator.ID()
	if typID == castID ||
		super.IsInteger(typID) && (super.IsInteger(castID) || super.IsFloat(castID)) ||
		super.IsFloat(typID) && super.IsFloat(castID) {
		return nil
	}
	return fmt.Errorf("type mismatch: %q cannot be used as %q", FormatType(typ), FormatType(decorator))
}

func (a *Analyzer) decorateEnum(val *Primitive, enumType *super.TypeEnum, decorator super.Type) (Value, error) {
	if val.typ != super.TypeString {
		return nil, fmt.Errorf("enum value must be string: %q", val.text)
	}
	if slices.Contains(enumType.Symbols, val.text) {
		return &Enum{
			typ:  decorator,
			name: val.text,
		}, nil
	}
	return nil, fmt.Errorf("symbol %q not a member of %s", val.text, FormatType(enumType))
}

func (a *Analyzer) decorateRecord(val *Record, decorator super.Type) (Value, error) {
	typ, ok := super.TypeUnder(decorator).(*super.TypeRecord)
	if !ok {
		return nil, fmt.Errorf("record decorator not a record: %q", FormatType(decorator))
	}
	if len(val.fields) != len(typ.Fields) {
		return nil, fmt.Errorf("record decorator incompatible with record value: %q", FormatType(typ))
	}
	fields := make([]Value, 0, len(val.fields))
	for k, f := range val.fields {
		val, err := a.decorate(f, typ.Fields[k].Type)
		if err != nil {
			return nil, err
		}
		fields = append(fields, val)
	}
	return &Record{
		typ:    decorator,
		fields: fields,
	}, nil
}

func (a Analyzer) decorateArray(array *Array, decorator super.Type) (Value, error) {
	typ, ok := super.TypeUnder(decorator).(*super.TypeArray)
	if !ok {
		return nil, fmt.Errorf("set decorator not an array: %q", FormatType(decorator))
	}
	elems := make([]Value, 0, len(array.elems))
	for _, elem := range array.elems {
		v, err := a.decorate(elem, typ.Type)
		if err != nil {
			return nil, err
		}
		elems = append(elems, v)
	}
	return &Array{
		typ:   decorator,
		elems: elems,
	}, nil
}

func (a Analyzer) decorateSet(set *Set, decorator super.Type) (Value, error) {
	typ, ok := super.TypeUnder(decorator).(*super.TypeSet)
	if !ok {
		return nil, fmt.Errorf("set decorator not a set: %q", FormatType(decorator))
	}
	elems := make([]Value, 0, len(set.elems))
	for _, elem := range set.elems {
		v, err := a.decorate(elem, typ.Type)
		if err != nil {
			return nil, err
		}
		elems = append(elems, v)
	}
	return &Set{
		typ:   decorator,
		elems: elems,
	}, nil
}

func (a Analyzer) decorateUnion(union *Union, decorator super.Type) (Value, error) {
	unionType, ok := super.TypeUnder(decorator).(*super.TypeUnion)
	if !ok {
		return nil, fmt.Errorf("union decorator not a union: %q", FormatType(decorator))
	}
	val := Value(union)
	if !super.IsTypeNamed(union.Type()) {
		// When a union is inside a union and it's not a named type,
		// the parent union is flattened and contains
		// this union's elemental types, so we just pull out the value from the child
		// union and cast it to the parent union.
		// When the value is a named union, we match the value exactly to the union types
		// since the named type won't be flattened in the union.
		val = union.value
	}
	valType := val.Type()
	if valType == unionType {
		return val, nil
	}
	for k, typ := range unionType.Types {
		if valType == typ {
			return &Union{
				typ:   decorator,
				tag:   k,
				value: val,
			}, nil
		}
	}
	return nil, fmt.Errorf("%q is not in union type %q", FormatType(valType), FormatType(unionType))
}

func (a Analyzer) decorateMap(m *Map, decorator super.Type) (Value, error) {
	typ, ok := super.TypeUnder(decorator).(*super.TypeMap)
	if !ok {
		return nil, fmt.Errorf("map decorator not a map: %q", FormatType(decorator))
	}
	entries := make([]Entry, 0, len(m.entries))
	for _, e := range m.entries {
		key, err := a.decorate(e.key, typ.KeyType)
		if err != nil {
			return nil, err
		}
		val, err := a.decorate(e.value, typ.ValType)
		if err != nil {
			return nil, err
		}
		entries = append(entries, Entry{key, val})
	}
	return &Map{
		typ:     decorator,
		entries: entries,
	}, nil
}

func (a Analyzer) decorateTypeValue(tv *TypeValue, decorator super.Type) (Value, error) {
	if _, ok := super.TypeUnder(decorator).(*super.TypeOfType); !ok {
		return nil, fmt.Errorf("cannot apply decorator (%q) to a type value", FormatType(decorator))
	}
	return &TypeValue{
		typ:   decorator,
		value: tv.value,
	}, nil
}

func (a Analyzer) decorateError(val *Error, decorator super.Type) (Value, error) {
	typ, ok := super.TypeUnder(decorator).(*super.TypeError)
	if !ok {
		return nil, fmt.Errorf("error decorator not an error type: %q", FormatType(decorator))
	}
	v, err := a.decorate(val.value, typ.Type)
	if err != nil {
		return nil, err
	}
	return &Error{
		typ:   decorator,
		value: v,
	}, nil
}

func (a Analyzer) convertType(typ ast.Type) (super.Type, error) {
	switch t := typ.(type) {
	case *ast.TypePrimitive:
		name := t.Name
		typ := super.LookupPrimitive(name)
		if typ == nil {
			return nil, fmt.Errorf("no such primitive type: %q", name)
		}
		return typ, nil
	case *ast.TypeRecord:
		return a.convertTypeRecord(t)
	case *ast.TypeArray:
		typ, err := a.convertType(t.Type)
		if err != nil {
			return nil, err
		}
		return a.sctx.LookupTypeArray(typ), nil
	case *ast.TypeSet:
		typ, err := a.convertType(t.Type)
		if err != nil {
			return nil, err
		}
		return a.sctx.LookupTypeSet(typ), nil
	case *ast.TypeMap:
		return a.convertTypeMap(t)
	case *ast.TypeUnion:
		return a.convertTypeUnion(t)
	case *ast.TypeEnum:
		return a.convertTypeEnum(t)
	case *ast.TypeError:
		typ, err := a.convertType(t.Type)
		if err != nil {
			return nil, err
		}
		return a.sctx.LookupTypeError(typ), nil
	case *ast.TypeRef:
		typ := a.sctx.LookupByName(t.Name)
		if typ == nil {
			return nil, fmt.Errorf("no such type name: %q", t.Name)
		}
		return typ, nil
	case *ast.TypeFusion:
		typ, err := a.convertType(t.Type)
		if err != nil {
			return nil, err
		}
		return a.sctx.LookupTypeFusion(typ)
	case *ast.TypeOption:
		typ, err := a.convertType(t.Type)
		if err != nil {
			return nil, err
		}
		if super.TypeUnder(typ) == super.TypeMissing {
			return nil, fmt.Errorf("option some type cannot be type missing")
		}
		return a.sctx.LookupTypeOption(typ), nil
	}
	return nil, fmt.Errorf("unknown type in Analyzer.convertType: %T", typ)
}

func (a Analyzer) convertTypeRecord(typ *ast.TypeRecord) (*super.TypeRecord, error) {
	fields := make([]super.Field, 0, len(typ.Fields))
	for _, f := range typ.Fields {
		typ, err := a.convertType(f.Type)
		if err != nil {
			return nil, err
		}
		if f.Opt {
			typ = a.sctx.Optionize(typ)
		}
		fields = append(fields, super.NewField(f.Name, typ))
	}
	return a.sctx.LookupTypeRecord(fields)
}

func (a Analyzer) convertTypeMap(tmap *ast.TypeMap) (*super.TypeMap, error) {
	keyType, err := a.convertType(tmap.KeyType)
	if err != nil {
		return nil, err
	}
	valType, err := a.convertType(tmap.ValType)
	if err != nil {
		return nil, err
	}
	return a.sctx.LookupTypeMap(keyType, valType), nil
}

var errAnonUnion = errors.New("anonymous union inside union")

func (a Analyzer) convertTypeUnion(union *ast.TypeUnion) (*super.TypeUnion, error) {
	var types []super.Type
	for _, typ := range union.Types {
		typ, err := a.convertType(typ)
		if err != nil {
			return nil, err
		}
		types = append(types, typ)
	}
	out, ok := a.sctx.LookupTypeUnion(types)
	if !ok {
		return nil, errAnonUnion
	}
	return out, nil
}

func (a Analyzer) convertTypeEnum(enum *ast.TypeEnum) (*super.TypeEnum, error) {
	if len(enum.Symbols) == 0 {
		return nil, errors.New("enum body is empty")
	}
	return a.sctx.LookupTypeEnum(symbolsOfEnum(enum)), nil
}

func symbolsOfEnum(enum *ast.TypeEnum) []string {
	var s []string
	for _, name := range enum.Symbols {
		s = append(s, name.Text)
	}
	return s
}
