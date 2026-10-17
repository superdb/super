package super

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/superdb/super/scode"
)

const (
	MaxEnumSymbols  = 100_000
	MaxRecordFields = 100_000
	MaxUnionTypes   = 100_000
)

type TypeFetcher interface {
	LookupType(id int) (Type, error)
}

// A Context implements the "type context" in the super data model.  For a
// given set of related Values, each Value has a type from a shared Context.
// The Context manages the transitive closure of Types so that each unique
// type corresponds to exactly one Type pointer allowing type equivalence
// to be determined by pointer comparison.  (Type pointers from distinct
// Contexts obviously do not have this property.)
type Context struct {
	mu        sync.RWMutex
	byID      map[uint32]Type
	typedefs  *TypeDefs
	named     map[string]*TypeNamed
	stringErr atomic.Pointer[TypeError]
	toValue   map[Type][]byte
	toType    map[string]Type
}

var _ TypeFetcher = (*Context)(nil)

func NewContext() *Context {
	return &Context{
		byID:     make(map[uint32]Type),
		typedefs: NewTypeDefs(),
	}
}

func (c *Context) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byID = make(map[uint32]Type)
	c.typedefs.Reset()
	c.toValue = nil
	c.toType = nil
	c.named = nil
}

func (c *Context) TypeDefs() *TypeDefs {
	return c.typedefs
}

func (c *Context) LookupType(id int) (Type, error) {
	if id < 0 {
		return nil, fmt.Errorf("type id (%d) cannot be negative", id)
	}
	if id < IDTypeComplex {
		return LookupPrimitiveByID(id)
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	typ, ok := c.byID[uint32(id)]
	if !ok {
		return nil, fmt.Errorf("no type found for type id %d", id)
	}
	return typ, nil
}

type DuplicateFieldError struct {
	Name string
}

func (d *DuplicateFieldError) Error() string {
	return fmt.Sprintf("duplicate field: %q", d.Name)
}

// LookupTypeRecord returns a TypeRecord within this context that binds with the
// indicated fields.  Subsequent calls with the same fields will return the
// same record pointer.  If the type doesn't exist, it's created, stored,
// and returned.  The closure of types within the fields must all be from
// this type context.  If you want to use fields from a different type context,
// use TranslateTypeRecord.
func (c *Context) LookupTypeRecord(fields []Field) (*TypeRecord, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.typedefs.LookupTypeRecord(fields)
	if typ, ok := c.byID[id]; ok {
		return typ.(*TypeRecord), nil
	}
	if name, ok := duplicateField(fields); ok {
		return nil, &DuplicateFieldError{name}
	}
	typ := NewTypeRecord(int(id), slices.Clone(fields))
	c.byID[id] = typ
	return typ, nil
}

var namesPool = sync.Pool{
	New: func() any {
		// Return a pointer to avoid allocation on conversion to
		// interface.
		names := make([]string, 8)
		return &names
	},
}

func duplicateField(fields []Field) (string, bool) {
	if len(fields) < 2 {
		return "", false
	}
	names := namesPool.Get().(*[]string)
	defer namesPool.Put(names)
	*names = (*names)[:0]
	for _, f := range fields {
		*names = append(*names, f.Name)
	}
	sort.Strings(*names)
	prev := (*names)[0]
	for _, n := range (*names)[1:] {
		if n == prev {
			return n, true
		}
		prev = n
	}
	return "", false
}

func (c *Context) MustLookupTypeRecord(fields []Field) *TypeRecord {
	r, err := c.LookupTypeRecord(fields)
	if err != nil {
		panic(err)
	}
	return r
}

func (c *Context) LookupTypeArray(inner Type) *TypeArray {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.typedefs.LookupTypeWrapped(TypeDefArray, inner)
	if typ, ok := c.byID[id]; ok {
		return typ.(*TypeArray)
	}
	typ := NewTypeArray(int(id), inner)
	c.byID[id] = typ
	return typ
}

func (c *Context) LookupTypeSet(inner Type) *TypeSet {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.typedefs.LookupTypeWrapped(TypeDefSet, inner)
	if typ, ok := c.byID[id]; ok {
		return typ.(*TypeSet)
	}
	typ := NewTypeSet(int(id), inner)
	c.byID[id] = typ
	return typ
}

func (c *Context) LookupTypeMap(keyType, valType Type) *TypeMap {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.typedefs.LookupTypeMap(keyType, valType)
	if typ, ok := c.byID[id]; ok {
		return typ.(*TypeMap)
	}
	typ := NewTypeMap(int(id), keyType, valType)
	c.byID[id] = typ
	return typ
}

func (c *Context) LookupTypeUnion(types []Type) (*TypeUnion, bool) {
	if badUnion(types) {
		return nil, false
	}
	sort.SliceStable(types, func(i, j int) bool {
		return CompareTypes(types[i], types[j]) < 0
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.typedefs.LookupTypeUnion(types)
	if typ, ok := c.byID[id]; ok {
		return typ.(*TypeUnion), true
	}
	typ := NewTypeUnion(int(id), slices.Clone(types))
	c.byID[id] = typ
	return typ, true
}

func badUnion(types []Type) bool {
	for _, t := range types {
		if _, ok := t.(*TypeUnion); ok {
			return true
		}
	}
	return false
}

func (c *Context) MustLookupTypeUnion(types []Type) *TypeUnion {
	typ, ok := c.LookupTypeUnion(types)
	if !ok {
		panic(types)
	}
	return typ
}

func (c *Context) UnionMissing(typ Type) *TypeUnion {
	if typ == TypeMissing {
		panic("cannot create none-only union")
	}
	var types []Type
	if union, ok := typ.(*TypeUnion); ok {
		for _, t := range union.Types {
			if t == TypeMissing {
				return union
			}
		}
		types = slices.Clone(union.Types)
	} else {
		types = []Type{typ}
	}
	return c.MustLookupTypeUnion(append(types, TypeMissing))
}

func (c *Context) LookupTypeEnum(symbols []string) *TypeEnum {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.typedefs.LookupTypeEnum(symbols)
	if typ, ok := c.byID[id]; ok {
		return typ.(*TypeEnum)
	}
	typ := NewTypeEnum(int(id), symbols)
	c.byID[id] = typ
	return typ
}

// LookupByName returns the named type last bound to name by LookupTypeNamed.
// It returns nil if name is unbound.
func (c *Context) LookupByName(name string) *TypeNamed {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.named == nil {
		return nil
	}
	return c.named[name]
}

// LookupTypeNamed returns the named type for name and inner.  It also binds
// name to that named type.  LookupTypeNamed returns an error if name is not a
// valid UTF-8 string or is a primitive type name.
func (c *Context) LookupTypeNamed(name string, inner Type) (*TypeNamed, error) {
	if !utf8.ValidString(name) {
		return nil, fmt.Errorf("bad type name %q: invalid UTF-8", name)
	}
	if err := CheckTypeName(name); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.named == nil {
		c.named = make(map[string]*TypeNamed)
	}
	if typ, ok := c.named[name]; ok {
		c.mu.Unlock()
		typ.Wait()
		if typ.Type != inner {
			return nil, fmt.Errorf("type %q redefined", name)
		}
		return typ, nil

	}
	c.mu.Unlock()
	named, patch, _ := c.DeclareTypeNamed(name)
	if patch < 0 {
		return named, nil
	}
	if err := c.BindTypeNamed(patch, named, inner); err != nil {
		return nil, err
	}
	return named, nil
}

func CheckTypeName(name string) error {
	if LookupPrimitive(name) != nil {
		return fmt.Errorf("named type collides with primitive type: %s", name)
	}
	if name == "any" {
		return fmt.Errorf("named type cannot be any")
	}
	return nil
}

func (c *Context) DeclareTypeNamed(name string) (*TypeNamed, int, error) {
	if err := CheckTypeName(name); err != nil {
		return nil, 0, err
	}
	c.mu.Lock()
	if c.named == nil {
		c.named = make(map[string]*TypeNamed)
	}
	// If this type already exists (or another thread is concurrently creating it),
	// we wait for the wrapped type to exist, then return completed type to the
	// caller with a patch of -1.  This indicates that the caller needs to
	// check that the types match instead of creating the inner and patching.
	if typ, ok := c.named[name]; ok {
		c.mu.Unlock()
		typ.Wait()
		return typ, -1, nil
	}
	// The name doesn't exist at all, so let's create an unresolved TypeNamed
	// that can be looked up and referenced but the inner Type cannot be used
	// until BindTypeNamed resolves it.
	defer c.mu.Unlock()
	id, patch, ok := c.typedefs.DeclareTypeNamed(name)
	if !ok {
		// This shouldn't happen because we would be waiting above if the
		// name already exists.
		panic(name)
	}
	// Create the unresolved named type with a nil wrapped type.
	typ := NewTypeNamed(int(id), name, nil)
	c.byID[id] = typ
	c.named[name] = typ
	return typ, patch, nil
}

func (c *Context) declareBlock(compIDs []compID) []patch {
	c.mu.Lock()
	if c.named == nil {
		c.named = make(map[string]*TypeNamed)
	}
	var patches []patch
	for _, cid := range compIDs {
		if typ, ok := c.named[cid.name]; ok {
			c.mu.Unlock()
			typ.Wait()
			c.mu.Lock()
			continue
		}
		id, off, ok := c.typedefs.DeclareTypeNamed(cid.name)
		if !ok {
			// This shouldn't happen because we would be waiting above if the
			// name already exists.
			c.mu.Unlock()
			panic(cid.name)
		}
		// Create the unresolved named type with a nil wrapped type.
		typ := NewTypeNamed(int(id), cid.name, nil)
		c.byID[id] = typ
		c.named[cid.name] = typ
		patches = append(patches, patch{named: typ, off: off, inner: cid.inner})
	}
	c.mu.Unlock()
	return patches
}

func (c *Context) BindTypeNamed(patch int, named *TypeNamed, inner Type) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	named.mu.Lock()
	defer named.mu.Unlock()
	if named.Type != nil {
		panic("type binding protocol violation")
	}
	// Set the inner type, patch the named typedef, and signal
	// any concurent threads waiting on this type's resolution.
	named.Type = inner
	c.typedefs.BindTypeNamed(patch, uint32(TypeID(inner)))
	named.cond.Broadcast()
	if containsFusion(inner) {
		return fmt.Errorf("named type %q cannot contain a fusion", named.Name)
	}
	return nil
}

func (c *Context) LookupTypeError(inner Type) *TypeError {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.typedefs.LookupTypeWrapped(TypeDefError, inner)
	if typ, ok := c.byID[id]; ok {
		return typ.(*TypeError)
	}
	typ := NewTypeError(int(id), inner)
	c.byID[id] = typ
	if inner == TypeString {
		c.stringErr.Store(typ)
	}
	return typ
}

func (c *Context) LookupTypeFusion(inner Type) (*TypeFusion, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.typedefs.LookupTypeWrapped(TypeDefFusion, inner)
	if typ, ok := c.byID[id]; ok {
		return typ.(*TypeFusion), nil
	}
	typ := NewTypeFusion(int(id), inner)
	c.byID[id] = typ
	return typ, nil
}

func (c *Context) MustLookupTypeFusion(inner Type) *TypeFusion {
	typ, err := c.LookupTypeFusion(inner)
	if err != nil {
		panic(err)
	}
	return typ
}

func (c *Context) LookupTypeOption(inner Type) *TypeOption {
	if _, ok := inner.(*TypeOption); ok || inner == TypeMissing {
		panic(inner)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.typedefs.LookupTypeWrapped(TypeDefOption, inner)
	if typ, ok := c.byID[id]; ok {
		return typ.(*TypeOption)
	}
	typ := NewTypeOption(int(id), inner)
	c.byID[id] = typ
	return typ
}

// LookupByValue returns the Type indicated by a binary-serialized type value.
// This provides a means to translate a type-context-independent serialized
// encoding for an arbitrary type into the reciever Context.
func (c *Context) LookupByValue(tv []byte) (Type, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.toType == nil {
		c.toType = make(map[string]Type)
		c.toValue = make(map[Type][]byte)
	}
	typ, ok := c.toType[string(tv)]
	if ok {
		return typ, nil
	}
	c.mu.Unlock()
	typ, rest := c.DecodeTypeValue(tv)
	c.mu.Lock()
	if rest == nil {
		return nil, errors.New("bad type value encoding")
	}
	c.toValue[typ] = slices.Clone(tv)
	c.toType[string(tv)] = typ
	return typ, nil
}

// TranslateType takes a type from another context and creates and returns that
// type in this context.
func (c *Context) TranslateType(ext Type) (Type, error) {
	return c.LookupByValue(EncodeTypeValue(ext))
}

func (c *Context) LookupTypeValue(typ Type) Value {
	c.mu.Lock()
	if c.toValue != nil {
		if bytes, ok := c.toValue[typ]; ok {
			c.mu.Unlock()
			return NewValue(TypeType, bytes)
		}
	}
	c.mu.Unlock()
	tv := EncodeTypeValue(typ)
	typ, err := c.LookupByValue(tv)
	if err != nil {
		// This shouldn't happen.
		return c.Missing()
	}
	return c.LookupTypeValue(typ)
}

func (c *Context) DecodeTypeValue(tv []byte) (Type, []byte) {
	if len(tv) == 0 {
		return nil, nil
	}
	id := tv[0]
	tv = tv[1:]
	switch id {
	case TypeValueNameDef:
		name, tv := DecodeName(tv)
		if tv == nil {
			return nil, nil
		}
		if named := c.LookupByName(name); named != nil {
			// This name already exists in the context.
			// Check that inner type is the same
			var inner Type
			inner, tv = c.DecodeTypeValue(tv)
			if tv == nil {
				return nil, nil
			}
			if inner != named.Type {
				return nil, nil
			}
			return named, tv
		}
		named, patch, err := c.DeclareTypeNamed(name)
		if err != nil {
			return nil, nil
		}
		var inner Type
		inner, tv = c.DecodeTypeValue(tv)
		if patch < 0 {
			// Another thread defined it first; just check that it matches.
			if tv == nil || inner != named.Type {
				return nil, nil
			}
			return named, tv
		}
		if tv == nil {
			c.BindTypeNamed(patch, named, TypeMissing) // release waiters
			return nil, nil
		}
		if err := c.BindTypeNamed(patch, named, inner); err != nil {
			return nil, nil
		}
		return named, tv
	case TypeValueNameRef:
		name, tv := DecodeName(tv)
		if tv == nil {
			return nil, nil
		}
		typ := c.LookupByName(name)
		if typ == nil {
			return nil, nil
		}
		return typ, tv
	case TypeValueRecord:
		n, tv := DecodeLength(tv)
		if tv == nil || n > MaxRecordFields {
			return nil, nil
		}
		fields := make([]Field, 0, n)
		for range n {
			var name string
			name, tv = DecodeName(tv)
			if tv == nil {
				return nil, nil
			}
			var typ Type
			typ, tv = c.DecodeTypeValue(tv)
			if tv == nil {
				return nil, nil
			}
			fields = append(fields, Field{name, typ})
		}
		typ, err := c.LookupTypeRecord(fields)
		if err != nil {
			return nil, nil
		}
		return typ, tv
	case TypeValueArray:
		inner, tv := c.DecodeTypeValue(tv)
		if tv == nil {
			return nil, nil
		}
		typ := c.LookupTypeArray(inner)
		if typ == nil {
			return nil, nil
		}
		return typ, tv
	case TypeValueSet:
		inner, tv := c.DecodeTypeValue(tv)
		if tv == nil {
			return nil, nil
		}
		typ := c.LookupTypeSet(inner)
		if typ == nil {
			return nil, nil
		}
		return typ, tv
	case TypeValueMap:
		keyType, tv := c.DecodeTypeValue(tv)
		if tv == nil {
			return nil, nil
		}
		valType, tv := c.DecodeTypeValue(tv)
		if tv == nil {
			return nil, nil
		}
		typ := c.LookupTypeMap(keyType, valType)
		if typ == nil {
			return nil, nil
		}
		return typ, tv
	case TypeValueUnion:
		n, tv := DecodeLength(tv)
		if tv == nil || n > MaxUnionTypes {
			return nil, nil
		}
		types := make([]Type, 0, n)
		for range n {
			var typ Type
			typ, tv = c.DecodeTypeValue(tv)
			types = append(types, typ)
		}
		typ, ok := c.LookupTypeUnion(types)
		if typ == nil || !ok {
			return nil, nil
		}
		return typ, tv
	case TypeValueEnum:
		n, tv := DecodeLength(tv)
		if tv == nil || n > MaxEnumSymbols {
			return nil, nil
		}
		var symbols []string
		for range n {
			var symbol string
			symbol, tv = DecodeName(tv)
			if tv == nil {
				return nil, nil
			}
			symbols = append(symbols, symbol)
		}
		typ := c.LookupTypeEnum(symbols)
		if typ == nil {
			return nil, nil
		}
		return typ, tv
	case TypeValueError:
		inner, tv := c.DecodeTypeValue(tv)
		if tv == nil {
			return nil, nil
		}
		typ := c.LookupTypeError(inner)
		if typ == nil {
			return nil, nil
		}
		return typ, tv
	case TypeValueFusion:
		inner, tv := c.DecodeTypeValue(tv)
		if tv == nil {
			return nil, nil
		}
		typ, err := c.LookupTypeFusion(inner)
		if typ == nil || err != nil {
			return nil, nil
		}
		return typ, tv
	case TypeValueOption:
		inner, tv := c.DecodeTypeValue(tv)
		if tv == nil {
			return nil, nil
		}
		typ := c.LookupTypeOption(inner)
		if typ == nil {
			return nil, nil
		}
		return typ, tv
	default:
		typ, err := LookupPrimitiveByID(int(id))
		if err != nil {
			return nil, nil
		}
		return typ, tv
	}
}

func DecodeName(tv []byte) (string, []byte) {
	namelen, tv := DecodeLength(tv)
	if tv == nil || namelen > len(tv) {
		return "", nil
	}
	return string(tv[:namelen]), tv[namelen:]
}

func MustDecodeName(tv []byte) (string, []byte) {
	namelen, tv := MustDecodeLength(tv)
	return string(tv[:namelen]), tv[namelen:]
}

func DecodeLength(tv []byte) (int, []byte) {
	namelen, n := binary.Uvarint(tv)
	if n <= 0 {
		return 0, nil
	}
	return int(namelen), tv[n:]
}

func MustDecodeLength(tv []byte) (int, []byte) {
	namelen, n := binary.Uvarint(tv)
	if n <= 0 {
		panic(tv)
	}
	return int(namelen), tv[n:]
}

func DecodeID(b []byte) (uint32, []byte) {
	v, n := binary.Uvarint(b)
	if n <= 0 {
		return 0, nil
	}
	return uint32(v), b[n:]
}

func MustDecodeID(b []byte) (uint32, []byte) {
	v, n := binary.Uvarint(b)
	if n <= 0 {
		panic(b)
	}
	return uint32(v), b[n:]
}

func (c *Context) Missing() Value {
	return NewValue(c.StringTypeError(), Missing)
}

func (c *Context) NewErrorf(format string, args ...any) Value {
	return NewValue(c.StringTypeError(), fmt.Appendf(nil, format, args...))
}

func (c *Context) NewError(err error) Value {
	return NewValue(c.StringTypeError(), []byte(err.Error()))
}

func (c *Context) StringTypeError() *TypeError {
	if typ := c.stringErr.Load(); typ != nil {
		return typ
	}
	return c.LookupTypeError(TypeString)
}

func (c *Context) WrapError(msg string, val Value) Value {
	recType := c.MustLookupTypeRecord([]Field{
		{"message", TypeString},
		{"on", val.Type()},
	})
	errType := c.LookupTypeError(recType)
	var b scode.Builder
	b.Append(EncodeString(msg))
	b.Append(val.Bytes())
	return NewValue(errType, b.Bytes())
}

func (c *Context) Nullable(typ Type) *TypeUnion {
	var types []Type
	if union, ok := typ.(*TypeUnion); ok {
		for _, t := range union.Types {
			if t == TypeNull {
				return union
			}
		}
		types = slices.Clone(union.Types)
	} else {
		types = []Type{typ}
	}
	return c.MustLookupTypeUnion(append(types, TypeNull))
}

func NullableUnion(typ Type) (*TypeUnion, int) {
	if union, ok := typ.(*TypeUnion); ok {
		for tag, typ := range union.Types {
			if typ == TypeNull {
				return union, tag
			}
		}
	}
	return nil, 0
}

// Optionize returns typ if typ is already an option type;
// otherwise, it returns a new option type for typ.
func (c *Context) Optionize(typ Type) Type {
	if _, ok := typ.(*TypeOption); ok {
		return typ
	}
	return c.LookupTypeOption(typ)
}

// TypeCache wraps a TypeFetcher with an unsynchronized cache for its LookupType
// method.  Cache hits incur none of the synchronization overhead of
// the underlying shared type context.
type TypeCache struct {
	cache   []Type
	fetcher TypeFetcher
}

var _ TypeFetcher = (*TypeCache)(nil)

func (t *TypeCache) Reset(fetcher TypeFetcher) {
	clear(t.cache)
	t.cache = t.cache[:0]
	t.fetcher = fetcher
}

func (t *TypeCache) LookupType(id int) (Type, error) {
	if id < len(t.cache) {
		if typ := t.cache[id]; typ != nil {
			return typ, nil
		}
	}
	typ, err := t.fetcher.LookupType(id)
	if err != nil {
		return nil, err
	}
	if id >= len(t.cache) {
		t.cache = slices.Grow(t.cache[:0], id+1)[:id+1]
	}
	t.cache[id] = typ
	return typ, nil
}

const (
	TypeDefRecord = 0
	TypeDefArray  = 1
	TypeDefSet    = 2
	TypeDefMap    = 3
	TypeDefUnion  = 4
	TypeDefEnum   = 5
	TypeDefError  = 6
	TypeDefNamed  = 7
	TypeDefFusion = 8
	TypeDefOption = 9
)

// Different modes for named types:
// (1) they are created from SUP or SuperSQL in which case the named type
// is first declared with an undefined inner type.  Let's let it be referenced
// for recursive type creation but not used so it then must be immediately bound
// after the inner type is created. TypeDefs itselfs handles this with DeclareTypeNamed
// followed by BindTypedName.
// (2) A Named type exists and is bound in a serialized TypeDefs and can be created
// with NewTypeDefsFromBytes. Nothing special need be done here.
// (3) A named type exists and is bound in a TypeDefs and is being copied with a
// mapper.  In this case, the mapper needs to do the decl followed by the
// binding in case recursive types are created.
// (4) A named type exists and is bound in a TypeDefs that is the source with a merger
// In this case, we can do the merge one-for-one into the target typedefs and there's
// no need to do a declare/bind sequence.

// TypeDefs encodes an interned set of types using type IDs that are
// local to this data structure.  This is used by Context to hold
// its type system and by fusion types and type values that implement
// vector.TypeLoader so that types may be materialized into the query
// Context on demand only when needed.  This data structure is designed
// to be serialized and deserialized as a whole into the BSUP format.
type TypeDefs struct {
	offsets []uint32
	bytes   []byte
	lut     map[string]uint32
	toID    map[Type]uint32
}

func NewTypeDefs() *TypeDefs {
	return &TypeDefs{
		offsets: make([]uint32, 1),
		lut:     make(map[string]uint32),
		toID:    make(map[Type]uint32),
	}
}

func (t *TypeDefs) Reset() {
	t.bytes = t.bytes[:0]
	t.offsets = t.offsets[:1]
	t.lut = make(map[string]uint32)
	clear(t.toID)
}

func (t *TypeDefs) Bytes() []byte {
	if t == nil {
		return nil
	}
	return t.bytes
}

func (t *TypeDefs) Serialization(id uint32) []byte {
	slot := id - IDTypeComplex
	return t.bytes[t.offsets[slot]:t.offsets[slot+1]]
}

func (t *TypeDefs) NTypes() int {
	return len(t.offsets) - 1
}

func (t *TypeDefs) Append(bytes []byte) uint32 {
	slot := uint32(len(t.offsets) - 1)
	t.bytes = append(t.bytes, bytes...)
	t.offsets = append(t.offsets, uint32(len(t.bytes)))
	return slot + IDTypeComplex
}

func (t *TypeDefs) AppendInPlace() uint32 {
	// Do an append but use the bytes that are poking off
	// the end as the new value and compute the slot from that.
	slot := uint32(len(t.offsets) - 1)
	t.offsets = append(t.offsets, uint32(len(t.bytes)))
	return slot + IDTypeComplex
}

func (t *TypeDefs) Lookup(at int) uint32 {
	key := string(t.bytes[at:])
	if key[0] == TypeDefNamed {
		// For named types we key on just {TypeDefNamed,name} and
		// ignore the ID pointer to the wrapped type. This way we hit
		// the same entry whether or not the wrapped type has been resolved
		// and backfilled, i.e., to support (mutually) recursive types.
		key = key[:len(key)-4]
	}
	id, ok := t.lut[key]
	if !ok {
		id = t.AppendInPlace()
		t.lut[key] = id
	} else {
		t.bytes = t.bytes[:at]
	}
	return id
}

func (t *TypeDefs) LookupType(ext Type) uint32 {
	if id := TypeID(ext); id < IDTypeComplex {
		return uint32(id)
	}
	id, ok := t.toID[ext]
	if ok {
		return id
	}
	switch ext := ext.(type) {
	case *TypeRecord:
		id = t.LookupTypeRecord(ext.Fields)
	case *TypeArray:
		id = t.LookupTypeWrapped(TypeDefArray, ext.Type)
	case *TypeSet:
		id = t.LookupTypeWrapped(TypeDefSet, ext.Type)
	case *TypeMap:
		id = t.LookupTypeMap(ext.KeyType, ext.ValType)
	case *TypeUnion:
		id = t.LookupTypeUnion(ext.Types)
	case *TypeEnum:
		id = t.LookupTypeEnum(ext.Symbols)
	case *TypeError:
		id = t.LookupTypeWrapped(TypeDefError, ext.Type)
	case *TypeNamed:
		id = t.LookupTypeNamed(ext.Name, ext.Type)
	case *TypeFusion:
		id = t.LookupTypeWrapped(TypeDefFusion, ext.Type)
	case *TypeOption:
		id = t.LookupTypeWrapped(TypeDefOption, ext.Type)
	default:
		panic(ext)
	}
	t.toID[ext] = id
	return id
}

func (t *TypeDefs) LookupTypeRecord(fields []Field) uint32 {
	// XXX change this to use pool for ids if profiling warrants
	var ids []uint32
	for _, f := range fields {
		ids = append(ids, t.LookupType(f.Type))
	}
	at := len(t.bytes)
	t.bytes = append(t.bytes, TypeDefRecord)
	t.bytes = binary.AppendUvarint(t.bytes, uint64(len(fields)))
	for k, f := range fields {
		t.bytes = binary.AppendUvarint(t.bytes, uint64(len(f.Name)))
		t.bytes = append(t.bytes, f.Name...)
		t.bytes = binary.AppendUvarint(t.bytes, uint64(ids[k]))
	}
	return t.Lookup(at)
}

func (t *TypeDefs) BindTypeRecord(names []string, fields []uint32, opts []bool) uint32 {
	at := len(t.bytes)
	t.bytes = append(t.bytes, TypeDefRecord)
	t.bytes = binary.AppendUvarint(t.bytes, uint64(len(fields)))
	for k, id := range fields {
		t.bytes = binary.AppendUvarint(t.bytes, uint64(len(names[k])))
		t.bytes = append(t.bytes, names[k]...)
		t.bytes = binary.AppendUvarint(t.bytes, uint64(id))
	}
	return t.Lookup(at)
}

func (t *TypeDefs) LookupTypeWrapped(typedef int, inner Type) uint32 {
	id := t.LookupType(inner)
	at := len(t.bytes)
	t.bytes = append(t.bytes, byte(typedef))
	t.bytes = binary.AppendUvarint(t.bytes, uint64(id))
	return t.Lookup(at)
}

func (t *TypeDefs) BindTypeWrapped(typedef int, inner uint32) uint32 {
	at := len(t.bytes)
	t.bytes = append(t.bytes, byte(typedef))
	t.bytes = binary.AppendUvarint(t.bytes, uint64(inner))
	return t.Lookup(at)
}

func (t *TypeDefs) LookupTypeMap(keyType, valType Type) uint32 {
	keyID := t.LookupType(keyType)
	valID := t.LookupType(valType)
	at := len(t.bytes)
	t.bytes = append(t.bytes, TypeDefMap)
	t.bytes = binary.AppendUvarint(t.bytes, uint64(keyID))
	t.bytes = binary.AppendUvarint(t.bytes, uint64(valID))
	return t.Lookup(at)
}

func (t *TypeDefs) LookupTypeUnion(types []Type) uint32 {
	sort.SliceStable(types, func(i, j int) bool {
		return CompareTypes(types[i], types[j]) < 0
	})
	// XXX change this to use pool for ids if profiling warrants
	var ids []uint32
	for _, typ := range types {
		id := t.LookupType(typ)
		ids = append(ids, id)
	}
	at := len(t.bytes)
	t.bytes = append(t.bytes, TypeDefUnion)
	t.bytes = binary.AppendUvarint(t.bytes, uint64(len(ids)))
	for _, id := range ids {
		t.bytes = binary.AppendUvarint(t.bytes, uint64(id))
	}
	return t.Lookup(at)
}

func (t *TypeDefs) LookupTypeEnum(symbols []string) uint32 {
	at := len(t.bytes)
	t.bytes = append(t.bytes, TypeDefEnum)
	t.bytes = binary.AppendUvarint(t.bytes, uint64(len(symbols)))
	for _, s := range symbols {
		t.bytes = binary.AppendUvarint(t.bytes, uint64(len(s)))
		t.bytes = append(t.bytes, s...)
	}
	return t.Lookup(at)
}

func (t *TypeDefs) LookupTypeNamed(name string, inner Type) uint32 {
	if id, ok := t.lookupName(name); ok {
		return id
	}
	// This can only be called after a name has been declared and
	// installed in the table.  It's up to the Context to make sure
	// the name gets declared then patched after the inner type
	// is computed.  Here, we just look up at the name.
	at := len(t.bytes)
	t.bytes = append(t.bytes, TypeDefNamed)
	t.bytes = binary.AppendUvarint(t.bytes, uint64(len(name)))
	t.bytes = append(t.bytes, name...)
	patch := len(t.bytes)
	t.bytes = AppendFixedID(t.bytes, 0xdeadbeef)
	id := t.Lookup(at)
	innerID := t.LookupType(inner)
	t.BindTypeNamed(patch, innerID)
	return id
}

func (t *TypeDefs) lookupName(name string) (uint32, bool) {
	scratch := make([]byte, 0, len(name)+1+binary.MaxVarintLen64)
	scratch = append(scratch, TypeDefNamed)
	scratch = binary.AppendUvarint(scratch, uint64(len(name)))
	scratch = append(scratch, name...)
	s, ok := t.lut[string(scratch)]
	return s, ok
}

func (t *TypeDefs) DeclareTypeNamed(name string) (uint32, int, bool) {
	at := len(t.bytes)
	t.bytes = append(t.bytes, TypeDefNamed)
	t.bytes = binary.AppendUvarint(t.bytes, uint64(len(name)))
	t.bytes = append(t.bytes, name...)
	if _, ok := t.lut[string(t.bytes[at:])]; ok {
		// Name already exists. Cannot redeclare it.
		return 0, 0, false
	}
	// Remember backfill position and leave space.
	patch := len(t.bytes)
	t.bytes = AppendFixedID(t.bytes, 0xdeadbeef)
	return t.Lookup(at), patch, true
}

func (t *TypeDefs) BindTypeNamed(patch int, id uint32) {
	PutFixedID(t.bytes[patch:patch+4], id)
}

type comp struct {
	inner uint32
	count int
}

type compID struct {
	name  string
	inner uint32
}

// Find the connected components of a possibly mutually recursive type.
// XXX instead of computing this for every named type lookup, we could
// change the typedefs format and store this information on write.
func (t *TypeDefs) conncomps(id uint32) []compID {
	scoreboard := make(map[string]comp)
	t.loops(id, scoreboard)
	var compIDs []compID
	for name, c := range scoreboard {
		if c.count > 1 {
			compIDs = append(compIDs, compID{name: name, inner: c.inner})
		}
	}
	return compIDs
}

func (t *TypeDefs) loops(id uint32, scoreboard map[string]comp) {
	if id < IDTypeComplex {
		return
	}
	b := t.Serialization(id)
	typedef := b[0]
	b = b[1:]
	switch typedef {
	case TypeDefNamed:
		name, b := MustDecodeName(b)
		id, b = DecodeFixedID(b)
		if b == nil {
			panic(name)
		}
		c, ok := scoreboard[name]
		if ok {
			if c.inner != id {
				panic(id)
			}
			scoreboard[name] = comp{inner: id, count: c.count + 1}
		} else {
			scoreboard[name] = comp{inner: id, count: 1}
			t.loops(id, scoreboard)
		}
	case TypeDefRecord:
		n, b := DecodeLength(b)
		if b == nil || n > MaxRecordFields {
			panic(typedef)
		}
		for range n {
			_, b = MustDecodeName(b)
			var id uint32
			id, b = DecodeID(b)
			t.loops(id, scoreboard)
		}
	case TypeDefArray:
		id, _ := MustDecodeID(b)
		t.loops(id, scoreboard)
	case TypeDefSet:
		id, _ := MustDecodeID(b)
		t.loops(id, scoreboard)
	case TypeDefMap:
		id, b := MustDecodeID(b)
		t.loops(id, scoreboard)
		id, b = MustDecodeID(b)
		t.loops(id, scoreboard)
	case TypeDefUnion:
		n, b := DecodeLength(b)
		if b == nil || n > MaxUnionTypes {
			panic(typedef)
		}
		for range n {
			var id uint32
			id, b = MustDecodeID(b)
			t.loops(id, scoreboard)
		}
	case TypeDefEnum:
	case TypeDefError:
		id, _ := MustDecodeID(b)
		t.loops(id, scoreboard)
	case TypeDefFusion:
		id, _ := MustDecodeID(b)
		t.loops(id, scoreboard)
	case TypeDefOption:
		id, _ := MustDecodeID(b)
		t.loops(id, scoreboard)
	default:
		panic(id)
	}
}

func (d *TypeDefs) AppendBytes(bytes []byte) bool {
	off := uint32(len(d.bytes))
	d.bytes = append(d.bytes, bytes...)
	localID := uint32(d.NTypes() + IDTypeComplex)
	for len(bytes) > 0 {
		before := bytes
		typedef := bytes[0]
		bytes = bytes[1:]
		var id uint32
		var n int
		switch typedef {
		case TypeDefNamed:
			_, bytes = MustDecodeName(bytes)
			if bytes == nil {
				return false
			}
			id, bytes = DecodeFixedID(bytes)
			if bytes == nil {
				return false
			}
		case TypeDefRecord:
			n, bytes = DecodeLength(bytes)
			if bytes == nil || n > MaxRecordFields {
				return false
			}
			for range n {
				_, bytes = DecodeName(bytes)
				if bytes == nil {
					return false
				}
				id, bytes = DecodeID(bytes)
				if bytes == nil || id >= localID {
					return false
				}
			}
		case TypeDefArray, TypeDefSet, TypeDefError, TypeDefFusion, TypeDefOption:
			id, bytes = DecodeID(bytes)
			if bytes == nil || id >= localID {
				return false
			}
		case TypeDefMap:
			// key ID
			id, bytes = DecodeID(bytes)
			if bytes == nil || id >= localID {
				return false
			}
			// val ID
			id, bytes = DecodeID(bytes)
			if bytes == nil || id >= localID {
				return false
			}
		case TypeDefUnion:
			n, bytes = DecodeLength(bytes)
			if bytes == nil || n > MaxUnionTypes {
				return false
			}
			for range n {
				id, bytes = DecodeID(bytes)
				if bytes == nil || id >= localID {
					return false
				}
			}
		case TypeDefEnum:
			n, bytes = DecodeLength(bytes)
			if bytes == nil || n > MaxEnumSymbols {
				return false
			}
			for range n {
				_, bytes = DecodeName(bytes)
				if bytes == nil {
					return false
				}
			}
		default:
			return false
		}
		size := len(before) - len(bytes)
		off += uint32(size)
		if before[0] == TypeDefNamed {
			size -= 4
		}
		d.lut[string(before[:size])] = localID
		d.offsets = append(d.offsets, off)
		localID++
	}
	return true
}

func AppendFixedID(b []byte, id uint32) []byte {
	b = append(b, byte(id>>24))
	b = append(b, byte(id>>16))
	b = append(b, byte(id>>8))
	b = append(b, byte(id))
	return b
}

func PutFixedID(b []byte, id uint32) {
	b[0] = byte(id >> 24)
	b[1] = byte(id >> 16)
	b[2] = byte(id >> 8)
	b[3] = byte(id)
}

func DecodeFixedID(b []byte) (uint32, []byte) {
	if len(b) < 4 {
		return 0, nil
	}
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	return v, b[4:]
}

type patch struct {
	named *TypeNamed
	off   int
	inner uint32
}

type TypeDefsMapper struct {
	*TypeDefs
	sctx    *Context
	cache   map[uint32]Type
	patches map[string]patch
}

func NewTypeDefsMapper(sctx *Context, defs *TypeDefs) *TypeDefsMapper {
	return &TypeDefsMapper{
		TypeDefs: defs,
		sctx:     sctx,
		cache:    make(map[uint32]Type),
	}
}

func (t *TypeDefsMapper) LookupType(id uint32) Type {
	typ, ok := t.cache[id]
	if !ok {
		typ = t.lookupType(id)
		t.cache[id] = typ
	}
	return typ
}

func (t *TypeDefsMapper) lookupType(id uint32) Type {
	if id < IDTypeComplex {
		t, _ := LookupPrimitiveByID(int(id))
		return t
	}
	b := t.Serialization(id)
	typedef := b[0]
	b = b[1:]
	switch typedef {
	case TypeDefNamed:
		name, b := DecodeName(b)
		if b == nil {
			return nil
		}
		if typ := t.sctx.LookupByName(name); typ != nil {
			return typ
		}
		if t.patches == nil {
			t.patches = make(map[string]patch)
		}
		// If we're already part of a loop analysis, then a call frame
		// somewhere up the stack is responsible for patching this type
		// so we can just return it here.
		if p, ok := t.patches[name]; ok {
			return p.named
		}
		var innerID uint32
		innerID, b = DecodeFixedID(b)
		if b == nil {
			return nil
		}
		// At this point, this name has not been declared and is not
		// in an existing mutually recursive loop that is being resolved.
		// So we can see if it initiates a loop, and otherwise, just resolve it.
		compIDs := t.conncomps(id)
		if len(compIDs) == 0 {
			// There's no loop to deal with...
			inner := t.LookupType(innerID)
			if inner == nil {
				return nil
			}
			out, err := t.sctx.LookupTypeNamed(name, inner)
			if err != nil {
				return nil
			}
			return out
		}
		patches := t.sctx.declareBlock(compIDs)
		for _, p := range patches {
			t.patches[p.named.Name] = p
		}
		// With the patches pending, we can call lookup on the inner ID now
		// knowing that all named types are resolved to at least a placeholder
		// type.
		inner := t.LookupType(innerID)
		// Now that LookupType was recursively called on in this inner type ID all
		// of the connected components will have been declared so we can go through
		// them all and do a lookup on the from-context inner ID and bind each one
		// to the resolved inner type in to-context.
		ok := inner != nil
		for _, p := range patches {
			pinner := t.LookupType(p.inner)
			if pinner == nil {
				ok = false
				pinner = TypeMissing // release waiters; this lookup fails below
			}
			if err := t.sctx.BindTypeNamed(p.off, p.named, pinner); err != nil {
				ok = false
			}
		}
		if !ok {
			return nil
		}
		// Finally we can lookup the named type now that it's inner type and all
		// connected components have been resolved.
		out, err := t.sctx.LookupTypeNamed(name, inner)
		if err != nil {
			return nil
		}
		return out
	case TypeDefRecord:
		n, b := DecodeLength(b)
		if b == nil || n > MaxRecordFields {
			return nil
		}
		fields := make([]Field, 0, n)
		for range n {
			var name string
			name, b = DecodeName(b)
			if b == nil {
				return nil
			}
			var id uint32
			id, b = DecodeID(b)
			typ := t.LookupType(id)
			if typ == nil {
				return nil
			}
			fields = append(fields, Field{name, typ})
		}
		typ, err := t.sctx.LookupTypeRecord(fields)
		if err != nil {
			return nil
		}
		return typ
	case TypeDefArray:
		var id uint32
		id, _ = DecodeID(b)
		inner := t.LookupType(id)
		if inner == nil {
			return nil
		}
		return t.sctx.LookupTypeArray(inner)
	case TypeDefSet:
		var id uint32
		id, _ = DecodeID(b)
		inner := t.LookupType(id)
		if inner == nil {
			return nil
		}
		return t.sctx.LookupTypeSet(inner)
	case TypeDefMap:
		var keyID, valID uint32
		keyID, b = DecodeID(b)
		if b == nil {
			return nil
		}
		valID, _ = DecodeID(b)
		keyType := t.LookupType(keyID)
		valType := t.LookupType(valID)
		if keyType == nil || valType == nil {
			return nil
		}
		return t.sctx.LookupTypeMap(keyType, valType)
	case TypeDefUnion:
		n, b := DecodeLength(b)
		if b == nil || n > MaxUnionTypes {
			return nil
		}
		types := make([]Type, 0, n)
		for range n {
			var id uint32
			id, b = DecodeID(b)
			typ := t.LookupType(id)
			if typ == nil {
				return nil
			}
			types = append(types, typ)
		}
		typ, ok := t.sctx.LookupTypeUnion(types)
		if !ok {
			return nil
		}
		return typ
	case TypeDefEnum:
		n, b := DecodeLength(b)
		if b == nil || n > MaxEnumSymbols {
			return nil
		}
		var symbols []string
		for range n {
			var symbol string
			symbol, b = DecodeName(b)
			if b == nil {
				return nil
			}
			symbols = append(symbols, symbol)
		}
		return t.sctx.LookupTypeEnum(symbols)
	case TypeDefError:
		id, b := DecodeID(b)
		if b == nil {
			return nil
		}
		inner := t.LookupType(id)
		if inner == nil {
			return nil
		}
		return t.sctx.LookupTypeError(inner)
	case TypeDefFusion:
		id, b := DecodeID(b)
		if b == nil {
			return nil
		}
		inner := t.LookupType(id)
		if inner == nil {
			return nil
		}
		typ, err := t.sctx.LookupTypeFusion(inner)
		if err != nil {
			return nil
		}
		return typ
	case TypeDefOption:
		id, b := DecodeID(b)
		if b == nil {
			return nil
		}
		inner := t.LookupType(id)
		if inner == nil {
			return nil
		}
		return t.sctx.LookupTypeOption(inner)
	default:
		panic(id)
	}
}

// A TypeDefsMerger recodes typedefs from an external table to a shared table
// on demand as external ID are looked up and converted to shared IDs.  This is
// used, for example, by the BSUP writer to collapse multiple typedefs tables
// into one table to be written to the BSUP metadata and copying only the typedefs
// that are used by subtypes in the serialized fusion vectors.  LookupID panics if any
// malformed data is encountered.
type TypeDefsMerger struct {
	*TypeDefs
	ext   *TypeDefs
	idmap map[uint32]uint32
}

func NewTypeDefsMerger(defs, ext *TypeDefs) *TypeDefsMerger {
	return &TypeDefsMerger{
		TypeDefs: defs,
		ext:      ext,
		idmap:    make(map[uint32]uint32),
	}
}

func (t *TypeDefsMerger) LookupID(extID uint32) uint32 {
	if extID < IDTypeComplex {
		return extID
	}
	if id, ok := t.idmap[extID]; ok {
		return id
	}
	bytes := t.ext.Serialization(extID)
	typedef := bytes[0]
	bytes = bytes[1:]
	var id uint32
	var n, at int
	var name string
	switch typedef {
	case TypeDefNamed:
		name, bytes = MustDecodeName(bytes)
		id, bytes = DecodeFixedID(bytes)
		if bytes == nil {
			panic(typedef)
		}
		id = t.LookupID(id)
		at = len(t.bytes)
		t.bytes = append(t.bytes, TypeDefNamed)
		t.bytes = binary.AppendUvarint(t.bytes, uint64(len(name)))
		t.bytes = append(t.bytes, name...)
		t.bytes = AppendFixedID(t.bytes, id)
	case TypeDefRecord:
		// extra alloc and copy due to recursion.  XXX put this in a pool.
		var out []byte
		n, bytes = MustDecodeLength(bytes)
		if n > MaxRecordFields {
			panic(n)
		}
		out = append(out, TypeDefRecord)
		out = binary.AppendUvarint(out, uint64(n))
		for range n {
			var name string
			name, bytes = MustDecodeName(bytes)
			id, bytes = MustDecodeID(bytes)
			out = binary.AppendUvarint(out, uint64(len(name)))
			out = append(out, name...)
			out = binary.AppendUvarint(out, uint64(t.LookupID(id)))
		}
		at = len(t.bytes)
		t.bytes = append(t.bytes, out...)
	case TypeDefArray:
		id, bytes = MustDecodeID(bytes)
		id = t.LookupID(id)
		at = len(t.bytes)
		t.bytes = append(t.bytes, TypeDefArray)
		t.bytes = binary.AppendUvarint(t.bytes, uint64(id))
	case TypeDefSet:
		id, bytes = MustDecodeID(bytes)
		id = t.LookupID(id)
		at = len(t.bytes)
		t.bytes = append(t.bytes, TypeDefSet)
		t.bytes = binary.AppendUvarint(t.bytes, uint64(id))
	case TypeDefMap:
		id, bytes = MustDecodeID(bytes)
		keyID := t.LookupID(id)
		id, bytes = MustDecodeID(bytes)
		valID := t.LookupID(id)
		at = len(t.bytes)
		t.bytes = append(t.bytes, TypeDefMap)
		t.bytes = binary.AppendUvarint(t.bytes, uint64(keyID))
		t.bytes = binary.AppendUvarint(t.bytes, uint64(valID))
	case TypeDefUnion:
		// extra alloc and copy due to recursion.  XXX put this in a pool.
		var out []byte
		n, bytes = MustDecodeLength(bytes)
		if n > MaxUnionTypes {
			panic(n)
		}
		out = append(out, TypeDefUnion)
		out = binary.AppendUvarint(out, uint64(n))
		for range n {
			id, bytes = MustDecodeID(bytes)
			out = binary.AppendUvarint(out, uint64(t.LookupID(id)))
		}
		at = len(t.bytes)
		t.bytes = append(t.bytes, out...)
	case TypeDefEnum:
		n, bytes = MustDecodeLength(bytes)
		at = len(t.bytes)
		t.bytes = append(t.bytes, TypeDefEnum)
		t.bytes = binary.AppendUvarint(t.bytes, uint64(n))
		for range n {
			name, bytes = MustDecodeName(bytes)
			t.bytes = binary.AppendUvarint(t.bytes, uint64(len(name)))
			t.bytes = append(t.bytes, name...)
		}
	case TypeDefError:
		id, bytes = MustDecodeID(bytes)
		id = t.LookupID(id)
		at = len(t.bytes)
		t.bytes = append(t.bytes, TypeDefError)
		t.bytes = binary.AppendUvarint(t.bytes, uint64(id))
	case TypeDefFusion:
		id, bytes = MustDecodeID(bytes)
		id = t.LookupID(id)
		at = len(t.bytes)
		t.bytes = append(t.bytes, TypeDefFusion)
		t.bytes = binary.AppendUvarint(t.bytes, uint64(id))
	case TypeDefOption:
		id, bytes = MustDecodeID(bytes)
		id = t.LookupID(id)
		at = len(t.bytes)
		t.bytes = append(t.bytes, TypeDefOption)
		t.bytes = binary.AppendUvarint(t.bytes, uint64(id))
	default:
		panic(id)
	}
	id = t.Lookup(at)
	t.idmap[extID] = id
	return id
}

// NewTypeDefsFromBytes takes a serialized representation of a typedefs table
// and computes the lookup table and offsets of each typedef in the table, returning
// a new TypeDefs table.  It checks that all referenced IDs in the table maintain
// the invariant that they are defined before use in the scan order of the table.
// It panics if malformed data is encountered.
func NewTypeDefsFromBytes(bytes []byte) (*TypeDefs, bool) {
	defs := NewTypeDefs()
	return defs, defs.AppendBytes(bytes)
}
