package super

import (
	"bytes"
	"errors"
	"sort"
	"sync"

	"github.com/superdb/super/scode"
)

type TypeArray struct {
	id   int
	Type Type
}

func NewTypeArray(id int, typ Type) *TypeArray {
	return &TypeArray{id, typ}
}

func (t *TypeArray) ID() int {
	return t.id
}

func (t *TypeArray) Kind() Kind {
	return ArrayKind
}

// ErrMissing is a Go error that implies a missing value in the runtime logic
// whereas Missing is an error value that represents a missing value embedded
// in the dataflow computation.
var ErrMissing = errors.New("missing")

// Missing is value that represents an error condition arising from a referenced
// entity not present, e.g., a reference to a non-existent record field, a map
// lookup for a key not present, an array index that is out of range, etc.
// The Missing error can be propagated through  functions and expressions and
// each operator has clearly defined semantics with respect to the Missing value.
// For example, "true AND MISSING" is MISSING.
var Missing = scode.Bytes("missing")

type TypeError struct {
	id   int
	Type Type
}

func NewTypeError(id int, typ Type) *TypeError {
	return &TypeError{id, typ}
}

func (t *TypeError) ID() int {
	return t.id
}

func (t *TypeError) Kind() Kind {
	return ErrorKind
}

func (t *TypeError) IsMissing(zv scode.Bytes) bool {
	return t.Type == TypeString && bytes.Equal(zv, Missing)
}

type TypeEnum struct {
	id      int
	Symbols []string
}

func NewTypeEnum(id int, symbols []string) *TypeEnum {
	return &TypeEnum{id, symbols}
}

func (t *TypeEnum) ID() int {
	return t.id
}

func (t *TypeEnum) Symbol(index int) (string, error) {
	if index < 0 || index >= len(t.Symbols) {
		return "", ErrEnumIndex
	}
	return t.Symbols[index], nil
}

func (t *TypeEnum) Lookup(symbol string) int {
	for k, s := range t.Symbols {
		if s == symbol {
			return k
		}
	}
	return -1
}

func (t *TypeEnum) Kind() Kind {
	return EnumKind
}

type TypeMap struct {
	id      int
	KeyType Type
	ValType Type
}

func NewTypeMap(id int, keyType, valType Type) *TypeMap {
	return &TypeMap{id, keyType, valType}
}

func (t *TypeMap) ID() int {
	return t.id
}

func (t *TypeMap) Kind() Kind {
	return MapKind
}

type keyval struct {
	key scode.Bytes
	val scode.Bytes
}

// NormalizeMap interprets zv as a map body and returns an equivalent map body
// that is normalized according to the BSUP specification (i.e., the tag-counted
// value of each entry's key is lexicographically greater than that of the
// preceding entry).
func NormalizeMap(zv scode.Bytes) scode.Bytes {
	elements := make([]keyval, 0, 8)
	for it := zv.Iter(); !it.Done(); {
		key := it.NextTagAndBody()
		val := it.NextTagAndBody()
		elements = append(elements, keyval{key, val})
	}
	if len(elements) < 2 {
		return zv
	}
	sort.Slice(elements, func(i, j int) bool {
		return bytes.Compare(elements[i].key, elements[j].key) == -1
	})
	norm := make(scode.Bytes, 0, len(zv))
	norm = append(norm, elements[0].key...)
	norm = append(norm, elements[0].val...)
	for i := 1; i < len(elements); i++ {
		// Skip duplicates.
		if !bytes.Equal(elements[i].key, elements[i-1].key) {
			norm = append(norm, elements[i].key...)
			norm = append(norm, elements[i].val...)
		}
	}
	return norm
}

type TypeNamed struct {
	id   int
	Name string
	// We protect the Type field with a condition variable since named types
	// may be initially declared without an inner type then bound.  The condition
	// is the presence of Type.  If multiple threads try to create the same
	// named type, this provides synchronization between creating the unbound
	// named type and the bound type and provides a means to reliably check
	// that the wrapped type is the same when the name is the same.
	mu   sync.Mutex
	cond *sync.Cond
	Type
}

func NewTypeNamed(id int, name string, typ Type) *TypeNamed {
	named := &TypeNamed{
		id:   id,
		Name: name,
		Type: typ,
	}
	named.cond = sync.NewCond(&named.mu)
	return named
}

func (t *TypeNamed) Wait() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for t.Type == nil {
		t.cond.Wait()
	}
}

func (t *TypeNamed) NamedID() int {
	return t.id
}

func TypeUnder(typ Type) Type {
	if named, ok := typ.(*TypeNamed); ok {
		return TypeUnder(named.Type)
	}
	return typ
}

// Field defines the name and type of a field for [TypeRecord].
type Field struct {
	Name string
	Type Type
}

func NewField(name string, typ Type) Field {
	return Field{name, typ}
}

type TypeRecord struct {
	id     int
	Fields []Field
	LUT    map[string]int
}

func NewTypeRecord(id int, fields []Field) *TypeRecord {
	if fields == nil {
		fields = []Field{}
	}
	r := &TypeRecord{
		id:     id,
		Fields: fields,
	}
	r.createLUT()
	return r
}

func (t *TypeRecord) ID() int {
	return t.id
}

func (t *TypeRecord) IndexOfField(field string) (int, bool) {
	v, ok := t.LUT[field]
	return v, ok
}

func (t *TypeRecord) Index(field string) Field {
	k, ok := t.LUT[field]
	if !ok {
		panic(field)
	}
	return t.Fields[k]
}

func (t *TypeRecord) TypeOfField(field string) (Type, bool) {
	n, ok := t.LUT[field]
	if !ok {
		return nil, false
	}
	return t.Fields[n].Type, true
}

func (t *TypeRecord) HasField(field string) bool {
	_, ok := t.LUT[field]
	return ok
}

func (t *TypeRecord) createLUT() {
	t.LUT = make(map[string]int)
	for k, f := range t.Fields {
		t.LUT[f.Name] = k
	}
}

func (t *TypeRecord) Kind() Kind {
	return RecordKind
}

type TypeSet struct {
	id   int
	Type Type
}

func NewTypeSet(id int, typ Type) *TypeSet {
	return &TypeSet{id, typ}
}

func (t *TypeSet) ID() int {
	return t.id
}

func (t *TypeSet) Kind() Kind {
	return SetKind
}

// NormalizeSet interprets zv as a set body and returns an equivalent set body
// that is normalized according to the BSUP specification (i.e., each element's
// tag-counted value is lexicographically greater than that of the preceding
// element).
func NormalizeSet(zv scode.Bytes) scode.Bytes {
	elements := make([]scode.Bytes, 0, 8)
	for it := zv.Iter(); !it.Done(); {
		elements = append(elements, it.NextTagAndBody())
	}
	if len(elements) < 2 {
		return zv
	}
	sort.Slice(elements, func(i, j int) bool {
		return bytes.Compare(elements[i], elements[j]) == -1
	})
	norm := make(scode.Bytes, 0, len(zv))
	norm = append(norm, elements[0]...)
	for i := 1; i < len(elements); i++ {
		// Skip duplicates.
		if !bytes.Equal(elements[i], elements[i-1]) {
			norm = append(norm, elements[i]...)
		}
	}
	return norm
}

type TypeFusion struct {
	id   int
	Type Type
}

func NewTypeFusion(id int, typ Type) *TypeFusion {
	return &TypeFusion{id: id, Type: typ}
}

func (t *TypeFusion) ID() int {
	return t.id
}

func (t *TypeFusion) Kind() Kind {
	return FusionKind
}

// Deref decodes the supertype value bytes and the subtype from a fusion type.
func (t *TypeFusion) Deref(sctx *Context, bytes scode.Bytes) (scode.Bytes, Type) {
	it := bytes.Iter()
	superBytes := it.Next()
	subType, err := sctx.LookupByValue(it.Next())
	if err != nil {
		panic(err)
	}
	return superBytes, subType
}

func (t *TypeFusion) DerefFusion(bytes scode.Bytes) (Type, scode.Bytes) {
	it := bytes.Iter()
	return t.Type, it.Next()
}

func BuildFusion(b *scode.Builder, superVal scode.Bytes, subType scode.Bytes) {
	b.BeginContainer()
	b.Append(superVal)
	b.Append(subType)
	b.EndContainer()
}

func IsTypeAny(typ Type) bool {
	fusion, ok := TypeUnder(typ).(*TypeFusion)
	return ok && fusion.Type == TypeAll
}

type TypeUnion struct {
	id    int
	Types []Type
	LUT   map[Type]int
}

func NewTypeUnion(id int, types []Type) *TypeUnion {
	t := &TypeUnion{id: id, Types: types}
	t.createLUT()
	return t
}

func (t *TypeUnion) createLUT() {
	t.LUT = make(map[Type]int)
	for i, typ := range t.Types {
		t.LUT[typ] = i
	}
}

func (t *TypeUnion) ID() int {
	return t.id
}

// Type returns the type corresponding to tag.
func (t *TypeUnion) Type(tag int) (Type, error) {
	if tag < 0 || tag >= len(t.Types) {
		return nil, ErrUnionTag
	}
	return t.Types[tag], nil
}

// TagOf returns the tag for typ in the union. If no type exists -1 is
// returned.
func (t *TypeUnion) TagOf(typ Type) int {
	if s, ok := t.LUT[typ]; ok {
		return s
	}
	return -1
}

// Untag takes bytes of the reciever's type and returns the underlying value
// as its type and bytes by removing the tag and determining that tag's
// type from the union.  Untag panics if the tag is invalid.
func (t *TypeUnion) Untag(bytes scode.Bytes) (Type, scode.Bytes) {
	it := bytes.Iter()
	tag := DecodeUint(it.Next())
	inner, err := t.Type(int(tag))
	if err != nil {
		panic(err)
	}
	return inner, it.Next()
}

func (t *TypeUnion) Kind() Kind {
	return UnionKind
}

// BuildUnion appends to b a union described by tag and val.
func BuildUnion(b *scode.Builder, tag int, val scode.Bytes) {
	BeginUnion(b, tag)
	b.Append(val)
	b.EndContainer()
}

func BeginUnion(b *scode.Builder, tag int) {
	b.BeginContainer()
	b.Append(EncodeUint(uint64(tag)))
}

const (
	OptionSomeTag = 0
	OptionNoneTag = 1
)

type TypeOption struct {
	id   int
	Type Type
}

func NewTypeOption(id int, typ Type) *TypeOption {
	return &TypeOption{id, typ}
}

func (t *TypeOption) ID() int {
	return t.id
}

func (t *TypeOption) Kind() Kind {
	return OptionKind
}

func (t *TypeOption) Decode(bytes scode.Bytes) (Type, scode.Bytes) {
	it := bytes.Iter()
	tag := DecodeUint(it.Next())
	if tag == OptionNoneTag {
		return nil, nil
	}
	return t.Type, it.Next()
}

func (t *TypeOption) Some(bytes scode.Bytes) Value {
	var b scode.Builder
	BuildSome(&b, bytes)
	return NewValue(t, b.Bytes().Body())
}

func (t *TypeOption) None() Value {
	var b scode.Builder
	BuildNone(&b)
	return NewValue(t, b.Bytes().Body())
}

func IsNone(t Type, bytes []byte) bool {
	typ := TypeUnder(t)
	if typ, ok := typ.(*TypeOption); ok {
		typ, _ := typ.Decode(bytes)
		return typ == nil
	}
	return false
}

func IsOptionType(typ Type) bool {
	_, ok := TypeUnder(typ).(*TypeOption)
	return ok
}

func BeginSomeContainer(b *scode.Builder) {
	b.BeginContainer()
	b.Append(EncodeUint(OptionSomeTag))
}

func BuildSome(b *scode.Builder, val scode.Bytes) {
	b.BeginContainer()
	b.Append(EncodeUint(OptionSomeTag))
	b.Append(val)
	b.EndContainer()
}

func BuildNone(b *scode.Builder) {
	b.BeginContainer()
	b.Append(EncodeUint(OptionNoneTag))
	b.EndContainer()
}

func Flatten(types []Type) []Type {
	out := make([]Type, 0, len(types))
	for _, t := range types {
		if union, ok := t.(*TypeUnion); ok {
			out = append(out, Flatten(union.Types)...)
		} else {
			out = append(out, t)
		}
	}
	return UniqueTypes(out)
}

func containsFusion(typ Type) bool {
	switch typ := typ.(type) {
	case *TypeFusion:
		// fusion(all) is the any type: each value carries its own type, so it
		// doesn't describe structure that the named barrier needs to protect.
		return typ.Type != TypeAll
	case *TypeRecord:
		for _, f := range typ.Fields {
			if containsFusion(f.Type) {
				return true
			}
		}
	case *TypeUnion:
		for _, t := range typ.Types {
			if containsFusion(t) {
				return true
			}
		}
	case *TypeArray:
		return containsFusion(typ.Type)
	case *TypeSet:
		return containsFusion(typ.Type)
	case *TypeMap:
		return containsFusion(typ.KeyType) || containsFusion(typ.ValType)
	case *TypeError:
		return containsFusion(typ.Type)
	case *TypeOption:
		return containsFusion(typ.Type)
	}
	return false
}
