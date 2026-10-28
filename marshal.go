package super

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/scode"
	"github.com/x448/float16"
)

//XXX handle new TypeError => marshal as a SUP string?

type CustomMarshaler interface {
	MarshalSuper(*Marshaler) (Type, error)
}

func Marshal(sctx *Context, v any) (Value, error) {
	return NewMarshaler(sctx).Marshal(v)
}

type Marshaler struct {
	sctx      *Context
	builder   scode.Builder
	decorator func(string, string) string
	bindings  map[string]string
}

func NewMarshaler(sctx *Context) *Marshaler {
	return &Marshaler{sctx: sctx}
}

// MarshalValue marshals v into the value that is being built and is
// typically called by a custom marshaler.
func (m *Marshaler) MarshalValue(v any) (Type, error) {
	return m.encodeValue(reflect.ValueOf(v))
}

func (m *Marshaler) Marshal(v any) (Value, error) {
	m.builder.Reset()
	typ, err := m.encodeValue(reflect.ValueOf(v))
	if err != nil {
		return Null, err
	}
	bytes := m.builder.Bytes()
	it := bytes.Iter()
	if it.Done() {
		return Null, errors.New("no value found")
	}
	return NewValue(typ, it.Next()), nil
}

func (m *Marshaler) MarshalCustom(names []string, vals []any) (Value, error) {
	if len(names) != len(vals) {
		return Null, errors.New("names and vals have different lengths")
	}
	m.builder.Reset()
	var fields []Field
	for k, v := range vals {
		typ, err := m.encodeValue(reflect.ValueOf(v))
		if err != nil {
			return Null, err
		}
		fields = append(fields, Field{Name: names[k], Type: typ})
	}
	// XXX issue #1836
	// Since this can be the inner loop here and nowhere else do we call
	// LookupTypeRecord on the inner loop, now may be the time to put an
	// efficient cache ahead of formatting the fields into a string,
	// e.g., compute a has in place across the field names then do a
	// closed-address exact match for the values in the slot.
	recType, err := m.sctx.LookupTypeRecord(fields)
	if err != nil {
		return Null, err
	}
	return NewValue(recType, m.builder.Bytes()), nil
}

const (
	tagName = "super"
	tagSep  = ","
)

func fieldName(f reflect.StructField) string {
	tag := f.Tag.Get(tagName)
	if tag == "" {
		tag = f.Tag.Get("json")
	}
	if tag != "" {
		s := strings.SplitN(tag, tagSep, 2)
		if len(s) > 0 && s[0] != "" {
			return s[0]
		}
	}
	return f.Name
}

func typeSimple(name, path string) string {
	return name
}

func typePackage(name, path string) string {
	a := strings.Split(path, "/")
	return fmt.Sprintf("%s.%s", a[len(a)-1], name)
}

func typeFull(name, path string) string {
	return fmt.Sprintf("%s.%s", path, name)
}

type TypeStyle int

const (
	StyleNone TypeStyle = iota
	StyleSimple
	StylePackage
	StyleFull
)

// Decorate informs the marshaler to add type decorations to the resulting BSUP
// in the form of named types in the sytle indicated, e.g.,
// for a `struct Foo` in `package bar` at import path `github.com/acme/bar:
// the corresponding name would be `Foo` for TypeSimple, `bar.Foo` for TypePackage,
// and `github.com/acme/bar.Foo`for TypeFull.  This mechanism works in conjunction
// with Bindings.  Typically you would want just one or the other, but if a binding
// doesn't exist for a given Go type, then a SUP type name will be created according
// to the decorator setting (which may be TypeNone).
func (m *Marshaler) Decorate(style TypeStyle) {
	switch style {
	default:
		m.decorator = nil
	case StyleSimple:
		m.decorator = typeSimple
	case StylePackage:
		m.decorator = typePackage
	case StyleFull:
		m.decorator = typeFull
	}
}

// NamedBindings informs the Marshaler to encode the given types with the
// corresponding SUP type names.  For example, to serialize a `bar.Foo`
// value decoroated with the SUP type name "SpecialFoo", simply call
// NamedBindings with the value []Binding{{"SpecialFoo", &bar.Foo{}}.
// Subsequent calls to NamedBindings
// add additional such bindings leaving the existing bindings in place.
// During marshaling, if no binding is found for a particular Go value,
// then the marshaler's decorator setting applies.
func (m *Marshaler) NamedBindings(bindings []Binding) error {
	if m.bindings == nil {
		m.bindings = make(map[string]string)
	}
	for _, b := range bindings {
		name, err := typeNameOfValue(b.Template)
		if err != nil {
			return err
		}
		m.bindings[name] = b.Name
	}
	return nil
}

var nanoTsType = reflect.TypeFor[nano.Ts]()
var superValueType = reflect.TypeFor[Value]()

func (m *Marshaler) encodeValue(v reflect.Value) (Type, error) {
	typ, err := m.encodeAny(v)
	if err != nil {
		return nil, err
	}
	if IsTypeNamed(typ) {
		// We already have a named type.
		return typ, nil
	}
	if !v.IsValid() {
		// v.Type will panic.
		return typ, nil
	}
	return m.lookupTypeNamed(v.Type(), typ)
}

func (m *Marshaler) encodeAny(v reflect.Value) (Type, error) {
	if !v.IsValid() {
		m.builder.Append(nil)
		return TypeNull, nil
	}
	switch v := v.Interface().(type) {
	case CustomMarshaler:
		return v.MarshalSuper(m)
	case float16.Float16:
		m.builder.Append(EncodeFloat16(v.Float32()))
		return TypeFloat16, nil
	case nano.Ts:
		m.builder.Append(EncodeTime(v))
		return TypeTimestamp, nil
	case net.IP:
		if a, err := netip.ParseAddr(v.String()); err == nil {
			m.builder.Append(EncodeIP(a))
			return TypeIP, nil
		}
	case uuid.UUID:
		m.builder.Append(EncodeUUID(v))
		return TypeUUID, nil
	case time.Time:
		m.builder.Append(EncodeTime(nano.TimeToTs(v)))
		return TypeTimestamp, nil
	case Type:
		val := m.sctx.LookupTypeValue(v)
		m.builder.Append(val.Bytes())
		return val.Type(), nil
	case Value:
		// Encode as {Fusion:<any>,Bytes:bytes,Subtype:typ}
		anyType := m.sctx.FusionAll()
		typeVal := m.sctx.LookupTypeValue(v.Type())
		BuildFusion(&m.builder, v.Bytes(), typeVal.Bytes())
		return anyType, nil
	}
	switch v.Kind() {
	case reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return m.encodeArrayBytes(v)
		}
		return m.encodeArray(v)
	case reflect.Map:
		if v.IsNil() {
			m.builder.Append(nil)
			return TypeNull, nil
		}
		return m.encodeMap(v)
	case reflect.Slice:
		if v.IsNil() {
			// XXX convert this to empty slice as scaffolding to pass tests.
			// in forthcoming PR, we will compute a recursive type for anything
			// that needs to be a named type and will have type info passed
			// down instead of bubbled up (though for concrete types we will
			// still bubble up)
			if v.Type().Elem().Kind() == reflect.Uint8 {
				m.builder.Append(nil)
				return TypeBytes, nil
			}
			m.builder.BeginContainer()
			m.builder.EndContainer()
			typ, err := m.lookupType(v.Type().Elem())
			if err != nil {
				return nil, err
			}
			return m.sctx.LookupTypeArray(typ), nil
		}
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return m.encodeSliceBytes(v)
		}
		return m.encodeArray(v)
	case reflect.Struct:
		if a, ok := v.Interface().(netip.Addr); ok {
			m.builder.Append(EncodeIP(a))
			return TypeIP, nil
		}
		return m.encodeRecord(v)
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			m.builder.Append(nil)
			return TypeNull, nil
		}
		return m.encodeValue(v.Elem())
	case reflect.String:
		m.builder.Append(EncodeString(v.String()))
		return TypeString, nil
	case reflect.Bool:
		m.builder.Append(EncodeBool(v.Bool()))
		return TypeBool, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		zt, err := m.lookupType(v.Type())
		if err != nil {
			return nil, err
		}
		m.builder.Append(EncodeInt(v.Int()))
		return zt, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		zt, err := m.lookupType(v.Type())
		if err != nil {
			return nil, err
		}
		m.builder.Append(EncodeUint(v.Uint()))
		return zt, nil
	case reflect.Float32:
		m.builder.Append(EncodeFloat32(float32(v.Float())))
		return TypeFloat32, nil
	case reflect.Float64:
		m.builder.Append(EncodeFloat64(v.Float()))
		return TypeFloat64, nil
	default:
		return nil, fmt.Errorf("unsupported type: %v", v.Kind())
	}
}

func (m *Marshaler) encodeMap(v reflect.Value) (Type, error) {
	var lastKeyType, lastValType Type
	m.builder.BeginContainer()
	for it := v.MapRange(); it.Next(); {
		keyType, err := m.encodeValue(it.Key())
		if err != nil {
			return nil, err
		}
		if keyType != lastKeyType && lastKeyType != nil {
			return nil, errors.New("map has mixed key types")
		}
		lastKeyType = keyType
		valType, err := m.encodeValue(it.Value())
		if err != nil {
			return nil, err
		}
		if valType != lastValType && lastValType != nil {
			return nil, errors.New("map has mixed values types")
		}
		lastValType = valType
	}
	m.builder.TransformContainer(NormalizeMap)
	m.builder.EndContainer()
	if lastKeyType == nil {
		// Map is empty so look up types.
		var err error
		lastKeyType, err = m.lookupType(v.Type().Key())
		if err != nil {
			return nil, err
		}
		lastValType, err = m.lookupType(v.Type().Elem())
		if err != nil {
			return nil, err
		}
	}
	return m.sctx.LookupTypeMap(lastKeyType, lastValType), nil
}

func (m *Marshaler) encodeRecord(sval reflect.Value) (Type, error) {
	m.builder.BeginContainer()
	var fields []Field
	stype := sval.Type()
	for i := range stype.NumField() {
		sf := stype.Field(i)
		isUnexported := sf.PkgPath != ""
		if sf.Anonymous {
			t := sf.Type
			if t.Kind() == reflect.Pointer {
				t = t.Elem()
			}
			if isUnexported && t.Kind() != reflect.Struct {
				// Ignore embedded fields of unexported non-struct types.
				continue
			}
			// Do not ignore embedded fields of unexported struct types
			// since they may have exported fields.
		} else if isUnexported {
			// Ignore unexported non-embedded fields.
			continue
		}
		field := stype.Field(i)
		name := fieldName(field)
		if name == "-" {
			// Ignore fields named "-".
			continue
		}
		typ, err := m.encodeValue(sval.Field(i))
		if err != nil {
			return nil, err
		}
		fields = append(fields, Field{Name: name, Type: typ})
	}
	m.builder.EndContainer()
	return m.sctx.LookupTypeRecord(fields)
}

func (m *Marshaler) encodeSliceBytes(sliceVal reflect.Value) (Type, error) {
	m.builder.Append(sliceVal.Bytes())
	return TypeBytes, nil
}

func (m *Marshaler) encodeArrayBytes(arrayVal reflect.Value) (Type, error) {
	n := arrayVal.Len()
	bytes := make([]byte, 0, n)
	for k := range n {
		v := arrayVal.Index(k)
		bytes = append(bytes, v.Interface().(uint8))
	}
	m.builder.Append(bytes)
	return TypeBytes, nil
}

func (m *Marshaler) encodeArray(arrayVal reflect.Value) (Type, error) {
	m.builder.BeginContainer()
	arrayLen := arrayVal.Len()
	types := make([]Type, 0, arrayLen)
	for i := range arrayLen {
		item := arrayVal.Index(i)
		typ, err := m.encodeValue(item)
		if err != nil {
			return nil, err
		}
		types = append(types, typ)
	}
	uniqueTypes := UniqueTypes(slices.Clone(types))
	var innerType Type
	switch len(uniqueTypes) {
	case 0:
		// if slice was empty, look up the type without a value
		var err error
		innerType, err = m.lookupType(arrayVal.Type().Elem())
		if err != nil {
			return nil, err
		}
	case 1:
		innerType = types[0]
	default:
		unionType := m.sctx.MustLookupTypeUnion(uniqueTypes)
		// Convert each container element to the union type.
		m.builder.TransformContainer(func(bytes scode.Bytes) scode.Bytes {
			var b scode.Builder
			for i, it := 0, bytes.Iter(); !it.Done(); i++ {
				BuildUnion(&b, unionType.TagOf(types[i]), it.Next())
			}
			return b.Bytes()
		})
		innerType = unionType
	}
	m.builder.EndContainer()
	return m.sctx.LookupTypeArray(innerType), nil
}

func (m *Marshaler) lookupType(t reflect.Type) (Type, error) {
	var typ Type
	switch t.Kind() {
	case reflect.Array, reflect.Slice:
		if t == uuidType {
			typ = TypeUUID
		} else if t.Elem().Kind() == reflect.Uint8 {
			typ = TypeBytes
		} else {
			inner, err := m.lookupType(t.Elem())
			if err != nil {
				return nil, err
			}
			typ = m.sctx.LookupTypeArray(inner)
		}
	case reflect.Map:
		key, err := m.lookupType(t.Key())
		if err != nil {
			return nil, err
		}
		val, err := m.lookupType(t.Elem())
		if err != nil {
			return nil, err
		}
		typ = m.sctx.LookupTypeMap(key, val)
	case reflect.Struct:
		var err error
		typ, err = m.lookupTypeRecord(t)
		if err != nil {
			return nil, err
		}
	case reflect.Pointer:
		var err error
		typ, err = m.lookupType(t.Elem())
		if err != nil {
			return nil, err
		}
	case reflect.String:
		typ = TypeString
	case reflect.Bool:
		typ = TypeBool
	case reflect.Int, reflect.Int64:
		typ = TypeInt64
	case reflect.Int32:
		typ = TypeInt32
	case reflect.Int16:
		typ = TypeInt16
	case reflect.Int8:
		typ = TypeInt8
	case reflect.Uint, reflect.Uint64:
		typ = TypeUint64
	case reflect.Uint32:
		typ = TypeUint32
	case reflect.Uint16:
		typ = TypeUint16
	case reflect.Uint8:
		typ = TypeUint8
	case reflect.Float32:
		typ = TypeFloat32
	case reflect.Float64:
		typ = TypeFloat64
	case reflect.Interface:
		// Encode Type as type any (aka fusion(all)) so that the types
		// for entities with embedded Values will not vary and otherwise
		// caused redefinition errors.  Otherwise, since we don't know the
		// underlying concrete type of interfaces, we encode them as the null type.
		if t.PkgPath() == "super" && t.Name() == "Type" {
			typ = m.sctx.FusionAll()
		} else {
			typ = TypeNull
		}
	default:
		return nil, fmt.Errorf("unsupported type: %v", t.Kind())
	}
	return m.lookupTypeNamed(t, typ)
}

func (m *Marshaler) lookupTypeRecord(structType reflect.Type) (Type, error) {
	var fields []Field
	for field := range structType.Fields() {
		name := fieldName(field)
		fieldType, err := m.lookupType(field.Type)
		if err != nil {
			return nil, err
		}
		fields = append(fields, Field{Name: name, Type: fieldType})
	}
	return m.sctx.LookupTypeRecord(fields)
}

// lookupTypeNamed returns a named type for typ with a name derived from t.  It
// returns typ if it shouldn't derive a name from t.
func (m *Marshaler) lookupTypeNamed(t reflect.Type, typ Type) (Type, error) {
	if m.decorator == nil && m.bindings == nil {
		return typ, nil
	}
	// Don't create named types for interface types as this is just
	// one value for that interface and it's the underlying concrete
	// types that implement the interface that we want to name.
	if t.Kind() == reflect.Interface {
		return typ, nil
	}
	// We do not want to further decorate nano.Ts as
	// it's already been converted to a time type;
	// likewise for Value, which gets encoded as
	// itself and its own named type if it has one.
	if t == nanoTsType || t == superValueType || t == netipAddrType || t == netIPType || t == uuidType {
		return typ, nil
	}
	name := t.Name()
	if name == "" || name == t.Kind().String() {
		return typ, nil
	}
	path := t.PkgPath()
	var named string
	if m.bindings != nil {
		named = m.bindings[typeFull(name, path)]
	}
	if named == "" && m.decorator != nil {
		named = m.decorator(name, path)
	}
	if named == "" {
		return typ, nil
	}
	return m.sctx.LookupTypeNamed(named, typ)
}

type CustomUnmarshaler interface {
	UnmarshalSuper(*Unmarshaler, Value) error
}

type Unmarshaler struct {
	sctx   *Context
	binder binder
}

func NewUnmarshaler() *Unmarshaler {
	return &Unmarshaler{}
}

func Unmarshal(val Value, v any) error {
	return NewUnmarshaler().decodeAny(val, reflect.ValueOf(v))
}

func incompatTypeError(typ Type, v reflect.Value) error {
	kind := typ.Kind()
	t := kind.String()
	if kind == PrimitiveKind {
		t = PrimitiveName(typ)
	}
	return fmt.Errorf("incompatible type translation: Super type %s, Go type %v, Go kind %v", t, v.Type(), v.Kind())
}

// SetContext provides an optional type context to the unmarshaler.  This is
// needed only when unmarshaling type values into Go Type interface values.
func (u *Unmarshaler) SetContext(sctx *Context) {
	u.sctx = sctx
}

func (u *Unmarshaler) Unmarshal(val Value, v any) error {
	return u.decodeAny(val, reflect.ValueOf(v))
}

// Bindings informs the unmarshaler that SUP values with a type name equal
// to any of the three variations of Go type mame (full path, package.Type,
// or just Type) may be used to inform the deserialization of a SUP value
// into a Go interface value.  If full path names are not used, it is up to
// the entitity that marshaled the original SUP to ensure that no type-name
// conflicts arise, e.g., when using the TypeSimple decorator style, you cannot
// have a type called bar.Foo and another type baz.Foo as the simple type
// decorator will be "Foo" in both cases and thus create a name conflict.
func (u *Unmarshaler) Bind(templates ...any) error {
	for _, t := range templates {
		if err := u.binder.enterTemplate(t); err != nil {
			return err
		}
	}
	return nil
}

func (u *Unmarshaler) NamedBindings(bindings []Binding) error {
	for _, b := range bindings {
		if err := u.binder.enterBinding(b); err != nil {
			return err
		}
	}
	return nil
}

var netipAddrType = reflect.TypeFor[netip.Addr]()
var netIPType = reflect.TypeFor[net.IP]()
var uuidType = reflect.TypeFor[uuid.UUID]()

func (u *Unmarshaler) decodeAny(val Value, v reflect.Value) (x error) {
	if !v.IsValid() {
		return errors.New("cannot unmarshal into value provided")
	}
	val = val.DeunionIntoNameds()
	m, v := indirect(v, val)
	if m != nil {
		return m.UnmarshalSuper(u, val)
	}
	switch v.Interface().(type) {
	case float16.Float16:
		if val.Type() != TypeFloat16 {
			return incompatTypeError(val.Type(), v)
		}
		v.SetUint(uint64(float16.Fromfloat32(float32(val.Float()))))
		return nil
	case nano.Ts:
		if val.Type() != TypeTimestamp {
			return incompatTypeError(val.Type(), v)
		}
		v.Set(reflect.ValueOf(DecodeTime(val.Bytes())))
		return nil
	case Value:
		// For Values we simply set the reflect value to the
		// a Value we create from the underlying Typeval/Bytes structure.
		fusionType, ok := val.Type().(*TypeFusion)
		if !ok || fusionType.Type != TypeAll {
			return errors.New("super value is not type fusion(all)")
		}
		//XXX
		if u.sctx == nil {
			u.sctx = NewContext()
		}
		bytes, typ := fusionType.Deref(u.sctx, val.Bytes())
		val := NewValue(typ, bytes)
		v.Set(reflect.ValueOf(val.Copy()))
		return nil
	}
	if v.Kind() == reflect.Pointer && val.IsNull() {
		v.Set(reflect.Zero(v.Type()))
		return nil
	}
	switch v.Kind() {
	case reflect.Array:
		if v.Type() == uuidType {
			return u.decodeUUID(val, v)
		}
		return u.decodeArray(val, v)
	case reflect.Map:
		return u.decodeMap(val, v)
	case reflect.Slice:
		if v.Type() == netIPType {
			return u.decodeNetIP(val, v)
		}
		return u.decodeArray(val, v)
	case reflect.Struct:
		if v.Type() == netipAddrType {
			return u.decodeNetipAddr(val, v)
		}
		return u.decodeRecord(val, v)
	case reflect.Interface:
		if TypeUnder(val.Type()) == TypeType {
			if u.sctx == nil {
				return errors.New("cannot unmarshal type value without type context")
			}
			typ, err := u.sctx.LookupByValue(val.Bytes())
			if err != nil {
				return err
			}
			v.Set(reflect.ValueOf(typ))
			return nil
		}
		// If the interface value isn't null, then the user has provided
		// an underlying value to unmarshal into.  So we just recursively
		// decode the value into this existing value and return.
		if !v.IsNil() {
			return u.decodeAny(val, v.Elem())
		}
		template, err := u.lookupGoType(val.Type(), val.Bytes())
		if err != nil {
			return err
		}
		if template == nil {
			// If the template is nil, then the value must be of BSUP type null
			// and BSUP type values can only have value null.  So, we
			// set it to null of the type given for the marshaled-into
			// value and return.
			v.Set(reflect.Zero(v.Type()))
			return nil
		}
		concrete := reflect.New(template)
		if err := u.decodeAny(val, concrete.Elem()); err != nil {
			return err
		}
		// For empty interface, we pull the value pointed-at into the
		// empty-interface value if it's not a struct (i.e., a scalar or
		// a slice)  For normal interfaces, we set the pointer to be
		// the pointer to the new object as it must be type-compatible.
		if v.NumMethod() == 0 && concrete.Elem().Kind() != reflect.Struct {
			v.Set(concrete.Elem())
		} else {
			v.Set(concrete)
		}
		return nil
	case reflect.String:
		// XXX We bundle string, type, error all into string.
		// See issue #1853.
		switch TypeUnder(val.Type()) {
		case TypeString, TypeType:
		default:
			return incompatTypeError(val.Type(), v)
		}
		v.SetString(DecodeString(val.Bytes()))
		return nil
	case reflect.Bool:
		if TypeUnder(val.Type()) != TypeBool {
			return incompatTypeError(val.Type(), v)
		}
		v.SetBool(val.Bool())
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch TypeUnder(val.Type()) {
		case TypeInt8, TypeInt16, TypeInt32, TypeInt64:
		default:
			return incompatTypeError(val.Type(), v)
		}
		v.SetInt(val.Int())
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		switch TypeUnder(val.Type()) {
		case TypeUint8, TypeUint16, TypeUint32, TypeUint64:
		default:
			return incompatTypeError(val.Type(), v)
		}
		v.SetUint(val.Uint())
		return nil
	case reflect.Float32:
		if TypeUnder(val.Type()) != TypeFloat32 {
			return incompatTypeError(val.Type(), v)
		}
		v.SetFloat(val.Float())
		return nil
	case reflect.Float64:
		if TypeUnder(val.Type()) != TypeFloat64 {
			return incompatTypeError(val.Type(), v)
		}
		v.SetFloat(val.Float())
		return nil
	default:
		return fmt.Errorf("unsupported type: %v", v.Kind())
	}
}

// Adapted from:
// https://github.com/golang/go/blob/46ab7a5c4f80d912f25b6b3e1044282a2a79df8b/src/encoding/json/decode.go#L426
func indirect(v reflect.Value, val Value) (CustomUnmarshaler, reflect.Value) {
	// If v is a named type and is addressable,
	// start with its address, so that if the type has pointer methods,
	// we find them.
	if v.Kind() != reflect.Pointer && v.Type().Name() != "" && v.CanAddr() {
		v = v.Addr()
	}
	var nilptr reflect.Value
	for v.Kind() == reflect.Pointer {
		if v.CanSet() && val.IsNull() {
			// If the reflect value can be set and the value is nil we want
			// to store this pointer since if destination is not a Value the
			// pointer will be set to nil.
			nilptr = v
		}
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		if v.Type().NumMethod() > 0 && v.CanInterface() {
			if u, ok := v.Interface().(CustomUnmarshaler); ok {
				return u, reflect.Value{}
			}
		}
		v = v.Elem()
	}
	if _, ok := v.Interface().(Value); !ok && nilptr.IsValid() {
		return nil, nilptr
	}
	return nil, v
}

func (u *Unmarshaler) decodeNetipAddr(val Value, v reflect.Value) error {
	if TypeUnder(val.Type()) != TypeIP {
		return incompatTypeError(val.Type(), v)
	}
	v.Set(reflect.ValueOf(DecodeIP(val.Bytes())))
	return nil
}

func (u *Unmarshaler) decodeUUID(val Value, v reflect.Value) error {
	switch TypeUnder(val.Type()) {
	case TypeUUID:
	case TypeBytes:
		// Accept the 16-byte form so UUIDs serialized as bytes before
		// the uuid type existed can still be decoded.
		if len(val.Bytes()) != 16 {
			return incompatTypeError(val.Type(), v)
		}
	default:
		return incompatTypeError(val.Type(), v)
	}
	v.Set(reflect.ValueOf(DecodeUUID(val.Bytes())))
	return nil
}

func (u *Unmarshaler) decodeNetIP(val Value, v reflect.Value) error {
	if TypeUnder(val.Type()) != TypeIP {
		return incompatTypeError(val.Type(), v)
	}
	v.Set(reflect.ValueOf(net.ParseIP(DecodeIP(val.Bytes()).String())))
	return nil
}

func (u *Unmarshaler) decodeMap(val Value, mapVal reflect.Value) error {
	if val.IsNull() {
		mapVal.Set(reflect.Zero(mapVal.Type()))
		return nil
	}
	typ, ok := TypeUnder(val.Type()).(*TypeMap)
	if !ok {
		return errors.New("not a map")
	}
	if mapVal.IsNil() {
		mapVal.Set(reflect.MakeMap(mapVal.Type()))
	}
	keyType := mapVal.Type().Key()
	valType := mapVal.Type().Elem()
	for it := val.ContainerIter(); !it.Done(); {
		key := reflect.New(keyType).Elem()
		if err := u.decodeAny(NewValue(typ.KeyType, it.Next()), key); err != nil {
			return err
		}
		val := reflect.New(valType).Elem()
		if err := u.decodeAny(NewValue(typ.ValType, it.Next()), val); err != nil {
			return err
		}
		mapVal.SetMapIndex(key, val)
	}
	return nil
}

func (u *Unmarshaler) decodeRecord(val Value, sval reflect.Value) error {
	recType, ok := TypeUnder(val.Type()).(*TypeRecord)
	if !ok {
		return fmt.Errorf("cannot unmarshal value into Go struct")
	}
	nameToField := make(map[string]int)
	stype := sval.Type()
	for i := range stype.NumField() {
		field := stype.Field(i)
		name := fieldName(field)
		nameToField[name] = i
	}
	for i, it := 0, val.Bytes().Iter(); !it.Done(); i++ {
		if i >= len(recType.Fields) {
			return errors.New("malformed super value")
		}
		itzv := it.Next()
		name := recType.Fields[i].Name
		if fieldIdx, ok := nameToField[name]; ok {
			typ := recType.Fields[i].Type
			if err := u.decodeAny(NewValue(typ, itzv), sval.Field(fieldIdx)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (u *Unmarshaler) decodeArray(val Value, arrVal reflect.Value) error {
	if val.IsNull() {
		arrVal.Set(reflect.Zero(arrVal.Type()))
		return nil
	}
	typ := TypeUnder(val.Type())
	if typ == TypeBytes && arrVal.Type().Elem().Kind() == reflect.Uint8 {
		if arrVal.Kind() == reflect.Array {
			return u.decodeArrayBytes(val, arrVal)
		}
		// arrVal is a slice here.
		arrVal.SetBytes(val.Bytes())
		return nil
	}
	arrType, ok := typ.(*TypeArray)
	if !ok {
		return fmt.Errorf("not an array")
	}
	i := 0
	for it := val.ContainerIter(); !it.Done(); i++ {
		itzv := it.Next()
		if i >= arrVal.Cap() {
			newcap := max(arrVal.Cap()+arrVal.Cap()/2, 4)
			newArr := reflect.MakeSlice(arrVal.Type(), arrVal.Len(), newcap)
			reflect.Copy(newArr, arrVal)
			arrVal.Set(newArr)
		}
		if i >= arrVal.Len() {
			arrVal.SetLen(i + 1)
		}
		if err := u.decodeAny(NewValue(arrType.Type, itzv), arrVal.Index(i)); err != nil {
			return err
		}
	}
	switch {
	case i == 0:
		arrVal.Set(reflect.MakeSlice(arrVal.Type(), 0, 0))
	case i < arrVal.Len():
		arrVal.SetLen(i)
	}
	return nil
}

func (u *Unmarshaler) decodeArrayBytes(val Value, arrayVal reflect.Value) error {
	if len(val.Bytes()) != arrayVal.Len() {
		return errors.New("BSUP bytes value length differs from Go array")
	}
	for k, b := range val.Bytes() {
		arrayVal.Index(k).Set(reflect.ValueOf(b))
	}
	return nil
}

type Binding struct {
	Name     string // user-defined name
	Template any    // zero-valued entity used as template for new such objects
}

type binding struct {
	key      string
	template reflect.Type
}

type binder map[string][]binding

func (b binder) lookup(name string) reflect.Type {
	if b == nil {
		return nil
	}
	for _, binding := range b[name] {
		if binding.key == name {
			return binding.template
		}
	}
	return nil
}

func (b *binder) enter(key string, typ reflect.Type) error {
	if *b == nil {
		*b = make(map[string][]binding)
	}
	slot := (*b)[key]
	entry := binding{
		key:      key,
		template: typ,
	}
	(*b)[key] = append(slot, entry)
	return nil
}

func (b *binder) enterTemplate(template any) error {
	typ, err := typeOfTemplate(template)
	if err != nil {
		return err
	}
	pkgPath := typ.PkgPath()
	path := strings.Split(pkgPath, "/")
	pkgName := path[len(path)-1]
	// e.g., Foo
	typeName := typ.Name()
	if err := b.enter(typeName, typ); err != nil {
		return err
	}
	// e.g., bar.Foo
	if err := b.enter(pkgName+"."+typeName, typ); err != nil {
		return err
	}
	// e.g., github.com/acme/pkg/bar.Foo
	return b.enter(pkgPath+"."+typeName, typ)
}

func (b *binder) enterBinding(binding Binding) error {
	typ, err := typeOfTemplate(binding.Template)
	if err != nil {
		return err
	}
	return b.enter(binding.Name, typ)
}

func typeOfTemplate(template any) (reflect.Type, error) {
	v := reflect.ValueOf(template)
	if !v.IsValid() {
		return nil, errors.New("invalid template")
	}
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	return v.Type(), nil
}

func typeNameOfValue(value any) (string, error) {
	typ, err := typeOfTemplate(value)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.%s", typ.PkgPath(), typ.Name()), nil
}

// lookupGoType builds a Go type for the super value given by typ and bytes.
// This process requires
// a value rather than a super type as it must determine the types of union elements
// from their tags.
func (u *Unmarshaler) lookupGoType(typ Type, bytes scode.Bytes) (reflect.Type, error) {
	switch typ := typ.(type) {
	case *TypeNamed:
		if template := u.binder.lookup(typ.Name); template != nil {
			return template, nil
		}
		// Ignore named types for which there are no bindings.
		// If an interface type being marshaled into doesn't
		// have a binding, then a type mismatch will be caught
		// by reflect when the Set() method is called on the
		// value and the concrete value doesn't implement the
		// interface.
		return u.lookupGoType(typ.Type, bytes)
	case *TypeRecord:
		return nil, errors.New("unmarshaling records into interface value requires type binding")
	case *TypeArray:
		// If we got here, we know the array type wasn't named and
		// therefore cannot have mixed-type elements.  So we don't need
		// to traverse the array and can just take the first element
		// as the template value to recurse upon.  If there are actually
		// heterogenous values, then the Go reflect package will raise
		// the problem when decoding the value.
		// If the inner type is a union, it must be a named-type union
		// so we know what Go type to use as the elements of the array,
		// which obviously can only be interface values for mixed types.
		// XXX there's a corner case here for union type where all the
		// elements of the array have the same tag, in which case you
		// can have a normal array of that tag's type.
		// We let the reflect package catch errors where the array contents
		// are not consistent.  All we need to do here is make sure the
		// interface name is in the bindings and the elemType will be
		// the appropriate interface type.
		it := bytes.Iter()
		if it.Done() {
			bytes = nil
		} else {
			bytes = it.Next()
		}
		elemType, err := u.lookupGoType(typ.Type, bytes)
		if err != nil {
			return nil, err
		}
		return reflect.SliceOf(elemType), nil
	case *TypeSet:
		// See comment above for TypeArray as it applies here.
		it := bytes.Iter()
		if it.Done() {
			bytes = nil
		} else {
			bytes = it.Next()
		}
		elemType, err := u.lookupGoType(typ.Type, bytes)
		if err != nil {
			return nil, err
		}
		return reflect.SliceOf(elemType), nil
	case *TypeUnion:
		return u.lookupGoType(typ.Untag(bytes))
	case *TypeEnum:
		// For now just return nil here. The layer above will flag
		// a type error.  At some point, we can create Go-native data structures
		// in package super for representing a union or enum as a standalone
		// entity.  See issue #1853.
		return nil, nil
	case *TypeMap:
		it := bytes.Iter()
		if it.Done() {
			return nil, fmt.Errorf("corrupt map value in super unmarshal")
		}
		keyType, err := u.lookupGoType(typ.KeyType, it.Next())
		if err != nil {
			return nil, err
		}
		if it.Done() {
			return nil, fmt.Errorf("corrupt map value in super unmarshal")
		}
		valType, err := u.lookupGoType(typ.ValType, it.Next())
		if err != nil {
			return nil, err
		}
		return reflect.MapOf(keyType, valType), nil
	default:
		return u.lookupPrimitiveType(typ)
	}
}

func (u *Unmarshaler) lookupPrimitiveType(typ Type) (reflect.Type, error) {
	var v any
	switch typ := typ.(type) {
	// XXX We should have counterparts for error and type type.
	// See issue #1853.
	// XXX udpate issue?
	case *TypeOfString, *TypeOfType:
		v = ""
	case *TypeOfBool:
		v = false
	case *TypeOfUint8:
		v = uint8(0)
	case *TypeOfUint16:
		v = uint16(0)
	case *TypeOfUint32:
		v = uint32(0)
	case *TypeOfUint64:
		v = uint64(0)
	case *TypeOfInt8:
		v = int8(0)
	case *TypeOfInt16:
		v = int16(0)
	case *TypeOfInt32:
		v = int32(0)
	case *TypeOfInt64:
		v = int64(0)
	case *TypeOfFloat16:
		v = float16.Fromfloat32(0)
	case *TypeOfFloat32:
		v = float32(0)
	case *TypeOfFloat64:
		v = float64(0)
	case *TypeOfIP:
		v = netip.Addr{}
	case *TypeOfUUID:
		v = uuid.UUID{}
	case *TypeOfNet:
		v = net.IPNet{}
	case *TypeOfTimestamp:
		v = time.Time{}
	case *TypeOfDuration:
		v = time.Duration(0)
	case *TypeOfNull:
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown Super type: %v", typ)
	}
	return reflect.TypeOf(v), nil
}
