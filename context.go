package super

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
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
	typedefs  *TypeDefsBuilder
	ids       []uint32 // scratch for type IDs (under mu)
	named     map[string]*TypeNamed
	stringErr atomic.Pointer[TypeError]
}

var _ TypeFetcher = (*Context)(nil)

func NewContext() *Context {
	return &Context{
		byID:     make(map[uint32]Type),
		typedefs: NewTypeDefsBuilder(),
	}
}

func (c *Context) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byID = make(map[uint32]Type)
	c.typedefs.Reset()
	c.named = nil
}

func (c *Context) TypeDefs() *TypeDefsBuilder {
	return c.typedefs
}

// TypeDefsSnapshot returns a read-only TypeDefs table of the types of c so
// far, in which each type has its type ID.  It shares c's storage without
// copying (see TypeDefsBuilder.Snapshot).
func (c *Context) TypeDefsSnapshot() *TypeDefs {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.typedefs.Snapshot()
}

// localID returns the type ID of typ and true if typ is a type of c (or a
// primitive type) and otherwise false.  It must be called with c.mu held.
//
// The type lookup methods (e.g., LookupTypeArray) accept types from other
// contexts (e.g., the field types of a record in another context), which they
// translate into c (see TranslateType) so that a type of c refers only to
// types of c.  Since that's rare, they check the types they're given with
// localID under c.mu and, if any is foreign, release c.mu, translate the
// types, and try again.
func (c *Context) localID(typ Type) (uint32, bool) {
	id := TypeID(typ)
	return uint32(id), id < IDTypeComplex || c.byID[uint32(id)] == typ
}

// mustTranslate returns TranslateType(typ) and panics if typ can't be
// translated into c (i.e., it has a named type whose name is bound to a
// different type in c).
func (c *Context) mustTranslate(typ Type) Type {
	typ, err := c.TranslateType(typ)
	if err != nil {
		panic(err)
	}
	return typ
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
	c.ids = c.ids[:0]
	for _, f := range fields {
		id, ok := c.localID(f.Type)
		if !ok {
			c.mu.Unlock()
			fields = slices.Clone(fields)
			for k := range fields {
				var err error
				if fields[k].Type, err = c.TranslateType(fields[k].Type); err != nil {
					return nil, err
				}
			}
			return c.LookupTypeRecord(fields)
		}
		c.ids = append(c.ids, id)
	}
	defer c.mu.Unlock()
	b := c.typedefs
	at := b.begin(TypeDefRecord)
	b.appendUvarint(uint64(len(fields)))
	for k, f := range fields {
		b.appendName(f.Name)
		b.appendUvarint(uint64(c.ids[k]))
	}
	id := b.Lookup(at)
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
	innerID, ok := c.localID(inner)
	if !ok {
		c.mu.Unlock()
		return c.LookupTypeArray(c.mustTranslate(inner))
	}
	defer c.mu.Unlock()
	id := c.typedefs.BindTypeWrapped(TypeDefArray, innerID)
	if typ, ok := c.byID[id]; ok {
		return typ.(*TypeArray)
	}
	typ := NewTypeArray(int(id), inner)
	c.byID[id] = typ
	return typ
}

func (c *Context) LookupTypeSet(inner Type) *TypeSet {
	c.mu.Lock()
	innerID, ok := c.localID(inner)
	if !ok {
		c.mu.Unlock()
		return c.LookupTypeSet(c.mustTranslate(inner))
	}
	defer c.mu.Unlock()
	id := c.typedefs.BindTypeWrapped(TypeDefSet, innerID)
	if typ, ok := c.byID[id]; ok {
		return typ.(*TypeSet)
	}
	typ := NewTypeSet(int(id), inner)
	c.byID[id] = typ
	return typ
}

func (c *Context) LookupTypeMap(keyType, valType Type) *TypeMap {
	c.mu.Lock()
	keyID, keyOK := c.localID(keyType)
	valID, valOK := c.localID(valType)
	if !keyOK || !valOK {
		c.mu.Unlock()
		return c.LookupTypeMap(c.mustTranslate(keyType), c.mustTranslate(valType))
	}
	defer c.mu.Unlock()
	id := c.typedefs.BindTypeMap(keyID, valID)
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
	c.ids = c.ids[:0]
	for _, typ := range types {
		id, ok := c.localID(typ)
		if !ok {
			c.mu.Unlock()
			// See localID.  Clone types since they may be a foreign
			// union's types.
			types = slices.Clone(types)
			for k, typ := range types {
				var err error
				if types[k], err = c.TranslateType(typ); err != nil {
					return nil, false
				}
			}
			return c.LookupTypeUnion(types)
		}
		c.ids = append(c.ids, id)
	}
	defer c.mu.Unlock()
	id := c.typedefs.BindTypeUnion(c.ids)
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
	id := c.typedefs.BindTypeEnum(symbols)
	if typ, ok := c.byID[id]; ok {
		return typ.(*TypeEnum)
	}
	typ := NewTypeEnum(int(id), symbols)
	c.byID[id] = typ
	return typ
}

// LookupTypeDecimal returns the decimal(precision,scale) type.  The caller
// must ensure the parameters are valid (see CheckDecimal).
func (c *Context) LookupTypeDecimal(precision, scale int) *TypeDecimal {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.typedefs.BindTypeDecimal(precision, scale)
	if typ, ok := c.byID[id]; ok {
		return typ.(*TypeDecimal)
	}
	typ := NewTypeDecimal(int(id), precision, scale)
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
	// Named types are types of the data model (values carry them) so any
	// in the body of a named type is fusion(all).  A type from another
	// context is translated into c so it can be compared with an existing
	// binding.
	inner, err := c.TranslateType(c.AnyToFusion(inner))
	if err != nil {
		return nil, err
	}
	if !utf8.ValidString(name) {
		return nil, fmt.Errorf("bad type name %q: invalid UTF-8", name)
	}
	named, patch, err := c.DeclareTypeNamed(name)
	if err != nil {
		return nil, err
	}
	if patch < 0 {
		if named.Type != inner {
			return nil, fmt.Errorf("type %q redefined", name)
		}
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
	// See LookupTypeNamed.  This is done before locking c as AnyToFusion
	// and TranslateType look up types in c.
	inner, err := c.TranslateType(c.AnyToFusion(inner))
	if err != nil {
		// Bind to missing so waiters are released and fail below.
		inner = TypeMissing
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	named.mu.Lock()
	defer named.mu.Unlock()
	if named.Type != nil {
		panic("type binding protocol violation")
	}
	cycle := namedCycle(named, inner)
	if cycle {
		// Bind to missing so waiters are released and fail below.
		inner = TypeMissing
	}
	innerID, ok := c.localID(inner)
	if !ok {
		panic("type binding protocol violation")
	}
	// Set the inner type, patch the named typedef, and signal
	// any concurent threads waiting on this type's resolution.
	named.Type = inner
	c.typedefs.BindTypeNamed(patch, innerID)
	named.cond.Broadcast()
	if err != nil {
		return err
	}
	if cycle {
		return fmt.Errorf("named type %q is bound to itself", named.Name)
	}
	if containsFusion(inner) {
		return fmt.Errorf("named type %q cannot contain a fusion", named.Name)
	}
	return nil
}

// namedCycle reports whether binding named to inner would create a cycle of
// named types with no other type in between (e.g., foo=foo or foo=bar,bar=foo),
// in which case the named types would have no underlying type.  A named type
// in the chain that isn't bound yet (e.g., while a block of mutually recursive
// types is being bound) ends the check, which is caught when that type is
// bound.  It must be called with c.mu held, under which named types are bound.
func namedCycle(named *TypeNamed, inner Type) bool {
	for {
		next, ok := inner.(*TypeNamed)
		if !ok {
			return false
		}
		if next == named {
			return true
		}
		if next.Type == nil {
			return false
		}
		inner = next.Type
	}
}

func (c *Context) LookupTypeError(inner Type) *TypeError {
	c.mu.Lock()
	innerID, ok := c.localID(inner)
	if !ok {
		c.mu.Unlock()
		return c.LookupTypeError(c.mustTranslate(inner))
	}
	defer c.mu.Unlock()
	id := c.typedefs.BindTypeWrapped(TypeDefError, innerID)
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
	innerID, ok := c.localID(inner)
	if !ok {
		c.mu.Unlock()
		inner, err := c.TranslateType(inner)
		if err != nil {
			return nil, err
		}
		return c.LookupTypeFusion(inner)
	}
	defer c.mu.Unlock()
	id := c.typedefs.BindTypeWrapped(TypeDefFusion, innerID)
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

// FusionAll returns fusion(all), the data model's type for a value of any
// type, which carries the value's own type.
func (c *Context) FusionAll() *TypeFusion {
	return c.MustLookupTypeFusion(TypeAll)
}

// AnyToFusion returns typ with each occurrence of any, which exists only in
// the type system, replaced by fusion(all) so that the result is a type of
// the data model.  A union with any as a member is replaced by fusion(all)
// as a value of fusion(all) carries its own type, which may be any member of
// the union.  The body of a named type never contains any (see
// LookupTypeNamed).
func (c *Context) AnyToFusion(typ Type) Type {
	if !HasAny(typ) {
		return typ
	}
	switch typ := typ.(type) {
	case *TypeOfAny:
		return c.FusionAll()
	case *TypeRecord:
		fields := make([]Field, len(typ.Fields))
		for k, f := range typ.Fields {
			fields[k] = NewField(f.Name, c.AnyToFusion(f.Type))
		}
		return c.MustLookupTypeRecord(fields)
	case *TypeArray:
		return c.LookupTypeArray(c.AnyToFusion(typ.Type))
	case *TypeSet:
		return c.LookupTypeSet(c.AnyToFusion(typ.Type))
	case *TypeMap:
		return c.LookupTypeMap(c.AnyToFusion(typ.KeyType), c.AnyToFusion(typ.ValType))
	case *TypeUnion:
		types := make([]Type, len(typ.Types))
		for k, t := range typ.Types {
			if _, ok := t.(*TypeOfAny); ok {
				return c.FusionAll()
			}
			types[k] = c.AnyToFusion(t)
		}
		return c.MustLookupTypeUnion(types)
	case *TypeError:
		return c.LookupTypeError(c.AnyToFusion(typ.Type))
	case *TypeOption:
		return c.LookupTypeOption(c.AnyToFusion(typ.Type))
	case *TypeFusion:
		inner := c.AnyToFusion(typ.Type)
		if IsFusionAll(inner) {
			return inner
		}
		return c.MustLookupTypeFusion(inner)
	}
	return typ
}

// HasAny returns true if typ contains any outside the body of a named type.
func HasAny(typ Type) bool {
	switch typ := typ.(type) {
	case *TypeOfAny:
		return true
	case *TypeRecord:
		return slices.ContainsFunc(typ.Fields, func(f Field) bool {
			return HasAny(f.Type)
		})
	case *TypeArray:
		return HasAny(typ.Type)
	case *TypeSet:
		return HasAny(typ.Type)
	case *TypeMap:
		return HasAny(typ.KeyType) || HasAny(typ.ValType)
	case *TypeUnion:
		return slices.ContainsFunc(typ.Types, HasAny)
	case *TypeError:
		return HasAny(typ.Type)
	case *TypeOption:
		return HasAny(typ.Type)
	case *TypeFusion:
		return HasAny(typ.Type)
	}
	return false
}

func (c *Context) LookupTypeOption(inner Type) *TypeOption {
	if _, ok := inner.(*TypeOption); ok || inner == TypeMissing || inner == TypeNone {
		panic(inner)
	}
	c.mu.Lock()
	innerID, ok := c.localID(inner)
	if !ok {
		c.mu.Unlock()
		return c.LookupTypeOption(c.mustTranslate(inner))
	}
	defer c.mu.Unlock()
	id := c.typedefs.BindTypeWrapped(TypeDefOption, innerID)
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
	typ, rest := c.DecodeTypeValue(tv)
	if rest == nil {
		return nil, errors.New("bad type value encoding")
	}
	return typ, nil
}

// TranslateType takes a type from another context and creates and returns that
// type in this context.  This is a slow path that encodes the type as a type
// value and decodes it in this context, which is fine since it's not used
// in hot paths.  A hot path should instead map types (or type IDs) with a
// TypeDefsMapper outside of the context.
func (c *Context) TranslateType(ext Type) (Type, error) {
	if c.isLocal(ext) {
		return ext, nil
	}
	typ, err := c.LookupByValue(EncodeTypeValue(ext))
	if err != nil {
		// Since ext is a valid type, its encoding fails to decode in c
		// when a named type in it is bound to a different type in c.
		return nil, errors.New("cannot translate type: conflicting named type binding")
	}
	return typ, nil
}

// LocalTypeID returns the type ID of typ in c, translating typ into c if it's
// from another context.
func (c *Context) LocalTypeID(typ Type) (uint32, error) {
	if !c.isLocal(typ) {
		var err error
		if typ, err = c.TranslateType(typ); err != nil {
			return 0, err
		}
	}
	return uint32(TypeID(typ)), nil
}

// isLocal reports whether typ is a type of c (or a primitive type).
func (c *Context) isLocal(typ Type) bool {
	id := TypeID(typ)
	if id < IDTypeComplex {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.byID[uint32(id)] == typ
}

// LookupTypeValue returns the type value of typ, which is translated into c
// if it's from another context.
func (c *Context) LookupTypeValue(typ Type) Value {
	if !c.isLocal(typ) {
		var err error
		if typ, err = c.TranslateType(typ); err != nil {
			// This shouldn't happen.
			return Missing
		}
	}
	return NewValue(TypeType, EncodeTypeValue(typ))
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
			if typ == nil {
				return nil, nil
			}
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
	case TypeValueDecimal:
		precision, tv := DecodeLength(tv)
		if tv == nil {
			return nil, nil
		}
		scale, tv := DecodeLength(tv)
		if tv == nil || CheckDecimal(precision, scale) != nil {
			return nil, nil
		}
		return c.LookupTypeDecimal(precision, scale), tv
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

// NewUnimplemented returns the value used in place of a value of type typ
// whose type is reserved but not yet implemented (see TypeOfUnimplemented and
// TypeDecimal).  The value is an error wrapping a record of the form
//
//	{unimplemented:<typ>,bytes:<bytes>}
//
// where bytes holds the value in typ's planned encoding so that, once typ is
// implemented, the original value can be recovered by unwrapping the error.
func (c *Context) NewUnimplemented(typ Type, bytes scode.Bytes) Value {
	recType := c.MustLookupTypeRecord([]Field{
		{"unimplemented", TypeType},
		{"bytes", TypeBytes},
	})
	errType := c.LookupTypeError(recType)
	var b scode.Builder
	b.Append(c.LookupTypeValue(typ).Bytes())
	b.Append(bytes)
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
	TypeDefRecord  = 0
	TypeDefArray   = 1
	TypeDefSet     = 2
	TypeDefMap     = 3
	TypeDefUnion   = 4
	TypeDefEnum    = 5
	TypeDefError   = 6
	TypeDefNamed   = 7
	TypeDefFusion  = 8
	TypeDefOption  = 9
	TypeDefDecimal = 10
)

// Different modes for named types:
// (1) they are created from SUP or SuperSQL in which case the named type
// is first declared with an undefined inner type.  Let's let it be referenced
// for recursive type creation but not used so it then must be immediately bound
// after the inner type is created. TypeDefsBuilder itself handles this with DeclareTypeNamed
// followed by BindTypeNamed.
// (2) A Named type exists and is bound in a serialized TypeDefs and can be created
// with NewTypeDefsFromBytes. Nothing special need be done here.
// (3) A named type exists and is bound in a TypeDefs and is being copied with a
// mapper.  In this case, the mapper needs to do the decl followed by the
// binding in case recursive types are created.
// (4) A named type exists and is bound in a TypeDefs that is the source with a merger.
// The merger likewise declares the name in the target typedefs before merging
// the inner type, which may refer to it, and then binds it.
//
// In all cases, a cycle of named types must pass through a type that isn't
// named (e.g., foo={l:foo|null} but not foo=foo or foo=bar,bar=foo) since
// otherwise the named types have no underlying type.

// TypeDefs is a read-only table of type definitions (typedefs) whose type IDs
// are local to the table, where the ID of the kth typedef is IDTypeComplex+k
// and a typedef refers to other types by their IDs.  A TypeDefs is looked up
// by ID, e.g., by a TypeDefsMapper to create the types in a Context, and is
// built by a TypeDefsBuilder, which interns the types.
//
// A TypeDefs is serialized (see Bytes) as the number of typedefs followed by
// two sections.  The first section is the offset of each typedef in the
// second section, in type ID order, where the number of typedefs and each
// offset are encoded as a uvarint, and the second section is the encoding of
// each typedef.  NewTypeDefsFromBytes creates a TypeDefs from its
// serialization, decoding only the offsets.
type TypeDefs struct {
	// offsets[k] is the offset in bytes of the typedef whose ID is
	// IDTypeComplex+k and the last offset is the end of the last typedef.
	offsets []uint32
	bytes   []byte
}

// NewTypeDefsFromBytes returns the TypeDefs serialized in bytes, which it
// references and does not copy.  It decodes the offsets of the typedefs but
// not the typedefs themselves, so a malformed typedef is detected only when
// it's looked up.  Empty bytes is an empty table.
func NewTypeDefsFromBytes(bytes []byte) (*TypeDefs, bool) {
	if len(bytes) == 0 {
		return &TypeDefs{offsets: []uint32{0}}, true
	}
	n, k := binary.Uvarint(bytes)
	// Each offset takes at least one byte.
	if k <= 0 || n > uint64(len(bytes)-k) {
		return nil, false
	}
	bytes = bytes[k:]
	offsets := make([]uint32, n+1)
	for i := range n {
		off, k := binary.Uvarint(bytes)
		if k <= 0 || off > math.MaxUint32 {
			return nil, false
		}
		bytes = bytes[k:]
		offsets[i] = uint32(off)
		// The first typedef starts the section and each typedef takes at
		// least one byte.
		if (i == 0 && off != 0) || (i > 0 && offsets[i] <= offsets[i-1]) {
			return nil, false
		}
	}
	if uint64(len(bytes)) > math.MaxUint32 || (n > 0 && uint32(len(bytes)) <= offsets[n-1]) {
		return nil, false
	}
	offsets[n] = uint32(len(bytes))
	return &TypeDefs{offsets: offsets, bytes: bytes}, true
}

// Bytes returns the serialization of t (see TypeDefs), which is empty for an
// empty table.
func (t *TypeDefs) Bytes() []byte {
	n := t.NTypes()
	if n == 0 {
		return nil
	}
	section := t.bytes[:t.offsets[n]]
	out := make([]byte, 0, binary.MaxVarintLen32*(n+1)+len(section))
	out = binary.AppendUvarint(out, uint64(n))
	for _, off := range t.offsets[:n] {
		out = binary.AppendUvarint(out, uint64(off))
	}
	return append(out, section...)
}

// Serialization returns the encoding of the typedef for id or nil if id isn't
// the ID of a typedef in t.
func (t *TypeDefs) Serialization(id uint32) []byte {
	if id < IDTypeComplex || int(id-IDTypeComplex) >= t.NTypes() {
		return nil
	}
	slot := id - IDTypeComplex
	return t.bytes[t.offsets[slot]:t.offsets[slot+1]]
}

func (t *TypeDefs) NTypes() int {
	if t == nil || len(t.offsets) == 0 {
		return 0
	}
	return len(t.offsets) - 1
}

// TypeDefsBuilder builds a TypeDefs from type IDs, interning each typedef by
// its encoding so there is one typedef for every type.  The table built so
// far is the embedded TypeDefs (e.g., &builder.TypeDefs), which reflects types
// as they're entered, and its serialization is available at any time from
// Bytes.  A TypeDefsEncoder enters types (as opposed to type IDs) in a
// builder.
type TypeDefsBuilder struct {
	TypeDefs
	lut map[string]uint32
}

func NewTypeDefsBuilder() *TypeDefsBuilder {
	return &TypeDefsBuilder{
		TypeDefs: TypeDefs{offsets: make([]uint32, 1)},
		lut:      make(map[string]uint32),
	}
}

func (t *TypeDefsBuilder) Reset() {
	t.bytes = t.bytes[:0]
	t.offsets = t.offsets[:1]
	t.lut = make(map[string]uint32)
}

// Snapshot returns a TypeDefs of the typedefs entered so far that shares t's
// storage without copying.  Since typedefs are only appended (to storage
// beyond the snapshot's), the snapshot remains valid as more typedefs are
// entered but not after Reset.  (The only typedef that changes is a named
// type declared but not yet bound by BindTypeNamed.)
func (t *TypeDefsBuilder) Snapshot() *TypeDefs {
	n := t.NTypes()
	end := t.offsets[n]
	return &TypeDefs{offsets: t.offsets[: n+1 : n+1], bytes: t.bytes[:end:end]}
}

func (t *TypeDefsBuilder) Append(bytes []byte) uint32 {
	slot := uint32(len(t.offsets) - 1)
	t.bytes = append(t.bytes, bytes...)
	t.offsets = append(t.offsets, uint32(len(t.bytes)))
	return slot + IDTypeComplex
}

func (t *TypeDefsBuilder) AppendInPlace() uint32 {
	// Do an append but use the bytes that are poking off
	// the end as the new value and compute the slot from that.
	slot := uint32(len(t.offsets) - 1)
	t.offsets = append(t.offsets, uint32(len(t.bytes)))
	return slot + IDTypeComplex
}

func (t *TypeDefsBuilder) Lookup(at int) uint32 {
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

// begin starts the encoding of a typedef with code typedef at the end of
// t.bytes and returns its position for Lookup.
func (t *TypeDefsBuilder) begin(typedef int) int {
	at := len(t.bytes)
	t.bytes = append(t.bytes, byte(typedef))
	return at
}

func (t *TypeDefsBuilder) appendUvarint(v uint64) {
	t.bytes = binary.AppendUvarint(t.bytes, v)
}

func (t *TypeDefsBuilder) appendName(name string) {
	t.bytes = binary.AppendUvarint(t.bytes, uint64(len(name)))
	t.bytes = append(t.bytes, name...)
}

// BindTypeRecord returns the ID of the record type whose fields have names
// and type IDs ids.
func (t *TypeDefsBuilder) BindTypeRecord(names []string, ids []uint32) uint32 {
	at := t.begin(TypeDefRecord)
	t.appendUvarint(uint64(len(ids)))
	for k, id := range ids {
		t.appendName(names[k])
		t.appendUvarint(uint64(id))
	}
	return t.Lookup(at)
}

// BindTypeWrapped returns the ID of the type with code typedef (i.e., an
// array, set, error, fusion, or option type) whose inner type has ID inner.
func (t *TypeDefsBuilder) BindTypeWrapped(typedef int, inner uint32) uint32 {
	at := t.begin(typedef)
	t.appendUvarint(uint64(inner))
	return t.Lookup(at)
}

func (t *TypeDefsBuilder) BindTypeMap(keyID, valID uint32) uint32 {
	at := t.begin(TypeDefMap)
	t.appendUvarint(uint64(keyID))
	t.appendUvarint(uint64(valID))
	return t.Lookup(at)
}

// BindTypeUnion returns the ID of the union type whose member types have IDs
// ids, which must be in the order of the member types (see CompareTypes).
func (t *TypeDefsBuilder) BindTypeUnion(ids []uint32) uint32 {
	at := t.begin(TypeDefUnion)
	t.appendUvarint(uint64(len(ids)))
	for _, id := range ids {
		t.appendUvarint(uint64(id))
	}
	return t.Lookup(at)
}

func (t *TypeDefsBuilder) BindTypeEnum(symbols []string) uint32 {
	at := t.begin(TypeDefEnum)
	t.appendUvarint(uint64(len(symbols)))
	for _, s := range symbols {
		t.appendName(s)
	}
	return t.Lookup(at)
}

func (t *TypeDefsBuilder) BindTypeDecimal(precision, scale int) uint32 {
	at := t.begin(TypeDefDecimal)
	t.appendUvarint(uint64(precision))
	t.appendUvarint(uint64(scale))
	return t.Lookup(at)
}

// unboundID is the inner type ID of a named type declared by
// DeclareTypeNamed but not yet bound by BindTypeNamed.
const unboundID = 0xdeadbeef

// namedInner returns the ID of the inner type of the named type with ID id,
// which is unboundID if it isn't bound yet.
func (t *TypeDefs) namedInner(id uint32) uint32 {
	b := t.Serialization(id)
	if len(b) == 0 || b[0] != TypeDefNamed {
		panic(id)
	}
	_, b = MustDecodeName(b[1:])
	inner, b := DecodeFixedID(b)
	if b == nil {
		panic(id)
	}
	return inner
}

// checkBinding panics if the named type with ID id and name is bound to a
// type other than the type with ID inner, i.e., the same name is bound to
// different types in the tables or contexts being encoded or merged.  Since
// typedefs are interned, the same type has the same ID.  A named type that's
// declared but not yet bound can't be checked.
func (t *TypeDefsBuilder) checkBinding(name string, id, inner uint32) {
	if bound := t.namedInner(id); bound != unboundID && bound != inner {
		panic(fmt.Errorf("type %q redefined", name))
	}
}

// LookupName returns the ID of the named type with name.
func (t *TypeDefsBuilder) LookupName(name string) (uint32, bool) {
	scratch := make([]byte, 0, len(name)+1+binary.MaxVarintLen64)
	scratch = append(scratch, TypeDefNamed)
	scratch = binary.AppendUvarint(scratch, uint64(len(name)))
	scratch = append(scratch, name...)
	s, ok := t.lut[string(scratch)]
	return s, ok
}

// DeclareTypeNamed enters the named type with name and returns its ID and the
// position of its inner type's ID, which must be set by BindTypeNamed (so the
// inner type may refer to the named type).  It returns false if name is
// already declared.
func (t *TypeDefsBuilder) DeclareTypeNamed(name string) (uint32, int, bool) {
	at := t.begin(TypeDefNamed)
	t.appendName(name)
	if _, ok := t.lut[string(t.bytes[at:])]; ok {
		// Name already exists. Cannot redeclare it.
		t.bytes = t.bytes[:at]
		return 0, 0, false
	}
	// Remember backfill position and leave space.
	patch := len(t.bytes)
	t.bytes = AppendFixedID(t.bytes, unboundID)
	return t.Lookup(at), patch, true
}

func (t *TypeDefsBuilder) BindTypeNamed(patch int, id uint32) {
	PutFixedID(t.bytes[patch:patch+4], id)
}

// TypeDefsEncoder encodes types from a Context into a TypeDefsBuilder,
// entering each type and the types it refers to in the builder and
// remembering the ID of each type it encodes.  (A Context doesn't need an
// encoder since its types' IDs are their IDs in its builder and it translates
// types from other contexts into itself.)
type TypeDefsEncoder struct {
	*TypeDefsBuilder
	toID map[Type]uint32
}

func NewTypeDefsEncoder() *TypeDefsEncoder {
	return &TypeDefsEncoder{
		TypeDefsBuilder: NewTypeDefsBuilder(),
		toID:            make(map[Type]uint32),
	}
}

func (t *TypeDefsEncoder) Reset() {
	t.TypeDefsBuilder.Reset()
	clear(t.toID)
}

// LookupType returns the ID of typ in the builder, entering it if needed.
func (t *TypeDefsEncoder) LookupType(typ Type) uint32 {
	if id := TypeID(typ); id < IDTypeComplex {
		return uint32(id)
	}
	if id, ok := t.toID[typ]; ok {
		return id
	}
	var id uint32
	switch typ := typ.(type) {
	case *TypeRecord:
		// XXX change this to use pool for names and ids if profiling warrants
		names := make([]string, 0, len(typ.Fields))
		ids := make([]uint32, 0, len(typ.Fields))
		for _, f := range typ.Fields {
			names = append(names, f.Name)
			ids = append(ids, t.LookupType(f.Type))
		}
		id = t.BindTypeRecord(names, ids)
	case *TypeArray:
		id = t.BindTypeWrapped(TypeDefArray, t.LookupType(typ.Type))
	case *TypeSet:
		id = t.BindTypeWrapped(TypeDefSet, t.LookupType(typ.Type))
	case *TypeMap:
		id = t.BindTypeMap(t.LookupType(typ.KeyType), t.LookupType(typ.ValType))
	case *TypeUnion:
		id = t.LookupTypeUnion(typ.Types)
	case *TypeEnum:
		id = t.BindTypeEnum(typ.Symbols)
	case *TypeDecimal:
		id = t.BindTypeDecimal(typ.Precision, typ.Scale)
	case *TypeError:
		id = t.BindTypeWrapped(TypeDefError, t.LookupType(typ.Type))
	case *TypeNamed:
		return t.lookupTypeNamed(typ)
	case *TypeFusion:
		id = t.BindTypeWrapped(TypeDefFusion, t.LookupType(typ.Type))
	case *TypeOption:
		id = t.BindTypeWrapped(TypeDefOption, t.LookupType(typ.Type))
	default:
		panic(typ)
	}
	t.toID[typ] = id
	return id
}

// LookupTypeUnion returns the ID of the union of types, sorting types in
// place.
func (t *TypeDefsEncoder) LookupTypeUnion(types []Type) uint32 {
	sort.SliceStable(types, func(i, j int) bool {
		return CompareTypes(types[i], types[j]) < 0
	})
	ids := make([]uint32, 0, len(types))
	for _, typ := range types {
		ids = append(ids, t.LookupType(typ))
	}
	return t.BindTypeUnion(ids)
}

func (t *TypeDefsEncoder) lookupTypeNamed(typ *TypeNamed) uint32 {
	// Remember the ID of typ before encoding its inner type, which may refer
	// to typ.
	if id, ok := t.LookupName(typ.Name); ok {
		// The name is already bound (e.g., by a type from another context),
		// so it must be bound to the same type here.
		t.toID[typ] = id
		t.checkBinding(typ.Name, id, t.LookupType(typ.Type))
		return id
	}
	// Declare the name before encoding the inner type, which may refer to it.
	id, patch, _ := t.DeclareTypeNamed(typ.Name)
	t.toID[typ] = id
	t.BindTypeNamed(patch, t.LookupType(typ.Type))
	return id
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
// It returns false if t is malformed.
// XXX instead of computing this for every named type lookup, we could
// change the typedefs format and store this information on write.
func (t *TypeDefs) conncomps(id uint32) ([]compID, bool) {
	scoreboard := make(map[string]comp)
	if !t.loops(id, scoreboard) {
		return nil, false
	}
	var compIDs []compID
	for name, c := range scoreboard {
		if c.count > 1 {
			compIDs = append(compIDs, compID{name: name, inner: c.inner})
		}
	}
	return compIDs, true
}

// loops updates scoreboard with the named types reachable from id and
// returns false if t is malformed.  Since the typedefs aren't validated when
// a TypeDefs is created, it checks that each type but a named type refers
// only to types that precede it so a malformed table can't cause unbounded
// recursion.
func (t *TypeDefs) loops(id uint32, scoreboard map[string]comp) bool {
	if id < IDTypeComplex {
		return true
	}
	b := t.Serialization(id)
	if len(b) == 0 {
		return false
	}
	typedef := b[0]
	b = b[1:]
	child := func(b []byte) ([]byte, bool) {
		var cid uint32
		cid, b = DecodeID(b)
		return b, b != nil && cid < id && t.loops(cid, scoreboard)
	}
	var ok bool
	switch typedef {
	case TypeDefNamed:
		name, b := DecodeName(b)
		if b == nil {
			return false
		}
		inner, b := DecodeFixedID(b)
		if b == nil {
			return false
		}
		if c, ok := scoreboard[name]; ok {
			if c.inner != inner {
				return false
			}
			scoreboard[name] = comp{inner: inner, count: c.count + 1}
			return true
		}
		scoreboard[name] = comp{inner: inner, count: 1}
		return t.loops(inner, scoreboard)
	case TypeDefRecord:
		n, b := DecodeLength(b)
		if b == nil || n > MaxRecordFields {
			return false
		}
		for range n {
			if _, b = DecodeName(b); b == nil {
				return false
			}
			if b, ok = child(b); !ok {
				return false
			}
		}
	case TypeDefArray, TypeDefSet, TypeDefError, TypeDefFusion, TypeDefOption:
		_, ok = child(b)
		return ok
	case TypeDefMap:
		if b, ok = child(b); !ok {
			return false
		}
		_, ok = child(b)
		return ok
	case TypeDefUnion:
		n, b := DecodeLength(b)
		if b == nil || n > MaxUnionTypes {
			return false
		}
		for range n {
			if b, ok = child(b); !ok {
				return false
			}
		}
	case TypeDefEnum, TypeDefDecimal:
	default:
		return false
	}
	return true
}

// namedChainOK reports whether following the inner type of the named type
// with ID id, and of each named type it's directly bound to, reaches a type
// that isn't named, i.e., the named types aren't in a cycle with no other
// type in between (see Context.BindTypeNamed).
func (t *TypeDefs) namedChainOK(id uint32) bool {
	// Without a cycle, each named typedef is visited at most once.
	for range t.NTypes() + 1 {
		b := t.Serialization(id)
		if len(b) == 0 || b[0] != TypeDefNamed {
			// A primitive or other type, or a bad ID, which is reported
			// when it's looked up.
			return true
		}
		if _, b = DecodeName(b[1:]); b == nil {
			return false
		}
		if id, b = DecodeFixedID(b); b == nil {
			return false
		}
	}
	return false
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

// TypeIDMap maps the type IDs in a TypeDefs table to type IDs in a Context on
// demand, caching each mapping.  Unlike a TypeDefsMapper, it's safe for
// concurrent use so it can be shared by the users of a table (e.g., the views
// of a vector.TypeValue).
type TypeIDMap struct {
	mu     sync.Mutex
	mapper *TypeDefsMapper
	// cache holds the type ID in the Context of each complex type ID in the
	// table that's been mapped and is otherwise 0, which is never the type ID
	// of a complex type.
	cache []atomic.Uint32
}

func NewTypeIDMap(sctx *Context, defs *TypeDefs) *TypeIDMap {
	return &TypeIDMap{
		mapper: NewTypeDefsMapper(sctx, defs),
		cache:  make([]atomic.Uint32, IDTypeComplex+defs.NTypes()),
	}
}

// TypeDefs returns the table whose type IDs m maps.
func (m *TypeIDMap) TypeDefs() *TypeDefs {
	return m.mapper.TypeDefs
}

// LookupID returns the type ID in the Context of the type with ID id in the
// table, or false if the type can't be mapped.
func (m *TypeIDMap) LookupID(id uint32) (uint32, bool) {
	if id < IDTypeComplex {
		// Primitive type IDs are the same in every context.
		return id, true
	}
	if int(id) < len(m.cache) {
		if local := m.cache[id].Load(); local != 0 {
			return local, true
		}
	}
	// The mapper isn't safe for concurrent use.
	m.mu.Lock()
	defer m.mu.Unlock()
	local, ok := m.mapper.LookupID(id)
	if ok && int(id) < len(m.cache) {
		// The table may have grown (e.g., if it's a TypeDefsBuilder's) so
		// only IDs within the cache are cached.
		m.cache[id].Store(local)
	}
	return local, ok
}

// LookupID returns the type ID in the mapper's Context of the type with ID id
// in its TypeDefs, or false if the typedef can't be mapped.
func (t *TypeDefsMapper) LookupID(id uint32) (uint32, bool) {
	typ := t.LookupType(id)
	if typ == nil {
		return 0, false
	}
	return uint32(TypeID(typ)), true
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
	if len(b) == 0 {
		return nil
	}
	typedef := b[0]
	b = b[1:]
	// Since the typedefs aren't validated when a TypeDefs is created, a type
	// but a named type must refer only to types that precede it so a malformed
	// table can't cause unbounded recursion.
	child := func(cid uint32) Type {
		if cid >= id {
			return nil
		}
		return t.LookupType(cid)
	}
	switch typedef {
	case TypeDefNamed:
		name, b := DecodeName(b)
		if b == nil {
			return nil
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
		if typ := t.sctx.LookupByName(name); typ != nil {
			// The name is already bound in sctx, so it must be bound to
			// the inner type here.  Cache the named type first since the
			// inner type may refer to it.
			typ.Wait()
			t.cache[id] = typ
			if inner := t.LookupType(innerID); inner == nil || t.sctx.AnyToFusion(inner) != typ.Type {
				delete(t.cache, id)
				return nil
			}
			return typ
		}
		// At this point, this name has not been declared and is not
		// in an existing mutually recursive loop that is being resolved.
		// So we can see if it initiates a loop, and otherwise, just resolve it.
		compIDs, ok := t.conncomps(id)
		if !ok {
			return nil
		}
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
		ok = inner != nil
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
			var cid uint32
			if cid, b = DecodeID(b); b == nil {
				return nil
			}
			typ := child(cid)
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
		cid, b := DecodeID(b)
		if b == nil {
			return nil
		}
		inner := child(cid)
		if inner == nil {
			return nil
		}
		return t.sctx.LookupTypeArray(inner)
	case TypeDefSet:
		cid, b := DecodeID(b)
		if b == nil {
			return nil
		}
		inner := child(cid)
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
		if valID, b = DecodeID(b); b == nil {
			return nil
		}
		keyType := child(keyID)
		valType := child(valID)
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
			var cid uint32
			if cid, b = DecodeID(b); b == nil {
				return nil
			}
			typ := child(cid)
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
	case TypeDefDecimal:
		precision, b := DecodeLength(b)
		if b == nil {
			return nil
		}
		scale, b := DecodeLength(b)
		if b == nil || CheckDecimal(precision, scale) != nil {
			return nil
		}
		return t.sctx.LookupTypeDecimal(precision, scale)
	case TypeDefError:
		cid, b := DecodeID(b)
		if b == nil {
			return nil
		}
		inner := child(cid)
		if inner == nil {
			return nil
		}
		return t.sctx.LookupTypeError(inner)
	case TypeDefFusion:
		cid, b := DecodeID(b)
		if b == nil {
			return nil
		}
		inner := child(cid)
		if inner == nil {
			return nil
		}
		typ, err := t.sctx.LookupTypeFusion(inner)
		if err != nil {
			return nil
		}
		return typ
	case TypeDefOption:
		cid, b := DecodeID(b)
		if b == nil {
			return nil
		}
		inner := child(cid)
		switch inner.(type) {
		case nil, *TypeOption:
			return nil
		}
		if inner == TypeMissing || inner == TypeNone {
			return nil
		}
		return t.sctx.LookupTypeOption(inner)
	default:
		// Malformed typedef.
		return nil
	}
}

// A TypeDefsMerger recodes typedefs from an external table to a shared table
// on demand as external ID are looked up and converted to shared IDs.  This is
// used, for example, by the BSUP writer to collapse multiple typedefs tables
// into one table to be written to the BSUP metadata and copying only the typedefs
// that are used by subtypes in the serialized fusion vectors.  LookupID panics if any
// malformed data is encountered.
type TypeDefsMerger struct {
	*TypeDefsBuilder
	ext   *TypeDefs
	idmap map[uint32]uint32
}

func NewTypeDefsMerger(defs *TypeDefsBuilder, ext *TypeDefs) *TypeDefsMerger {
	return &TypeDefsMerger{
		TypeDefsBuilder: defs,
		ext:             ext,
		idmap:           make(map[uint32]uint32),
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
	if len(bytes) == 0 {
		panic(extID)
	}
	typedef := bytes[0]
	bytes = bytes[1:]
	var id uint32
	var n, at int
	var name string
	switch typedef {
	case TypeDefNamed:
		name, bytes = MustDecodeName(bytes)
		var inner uint32
		inner, bytes = DecodeFixedID(bytes)
		if bytes == nil {
			panic(typedef)
		}
		if id, ok := t.LookupName(name); ok {
			// The name is already bound in the target, so it must be bound
			// to the same type here.  Record its ID before merging the inner
			// type, which may refer to it.
			t.idmap[extID] = id
			t.checkBinding(name, id, t.LookupID(inner))
			return id
		}
		if !t.ext.namedChainOK(extID) {
			panic(extID)
		}
		// Declare the name and record its ID before mapping the inner type,
		// which may refer to it (i.e., the type is recursive).
		id, patch, _ := t.DeclareTypeNamed(name)
		t.idmap[extID] = id
		t.BindTypeNamed(patch, t.LookupID(inner))
		return id
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
			out = binary.AppendUvarint(out, uint64(t.lookupChild(id, extID)))
		}
		at = len(t.bytes)
		t.bytes = append(t.bytes, out...)
	case TypeDefArray:
		id, bytes = MustDecodeID(bytes)
		id = t.lookupChild(id, extID)
		at = len(t.bytes)
		t.bytes = append(t.bytes, TypeDefArray)
		t.bytes = binary.AppendUvarint(t.bytes, uint64(id))
	case TypeDefSet:
		id, bytes = MustDecodeID(bytes)
		id = t.lookupChild(id, extID)
		at = len(t.bytes)
		t.bytes = append(t.bytes, TypeDefSet)
		t.bytes = binary.AppendUvarint(t.bytes, uint64(id))
	case TypeDefMap:
		id, bytes = MustDecodeID(bytes)
		keyID := t.lookupChild(id, extID)
		id, bytes = MustDecodeID(bytes)
		valID := t.lookupChild(id, extID)
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
			out = binary.AppendUvarint(out, uint64(t.lookupChild(id, extID)))
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
		id = t.lookupChild(id, extID)
		at = len(t.bytes)
		t.bytes = append(t.bytes, TypeDefError)
		t.bytes = binary.AppendUvarint(t.bytes, uint64(id))
	case TypeDefFusion:
		id, bytes = MustDecodeID(bytes)
		id = t.lookupChild(id, extID)
		at = len(t.bytes)
		t.bytes = append(t.bytes, TypeDefFusion)
		t.bytes = binary.AppendUvarint(t.bytes, uint64(id))
	case TypeDefOption:
		id, bytes = MustDecodeID(bytes)
		id = t.lookupChild(id, extID)
		at = len(t.bytes)
		t.bytes = append(t.bytes, TypeDefOption)
		t.bytes = binary.AppendUvarint(t.bytes, uint64(id))
	case TypeDefDecimal:
		var precision, scale int
		precision, bytes = MustDecodeLength(bytes)
		scale, bytes = MustDecodeLength(bytes)
		at = len(t.bytes)
		t.bytes = append(t.bytes, TypeDefDecimal)
		t.bytes = binary.AppendUvarint(t.bytes, uint64(precision))
		t.bytes = binary.AppendUvarint(t.bytes, uint64(scale))
	default:
		panic(id)
	}
	id = t.Lookup(at)
	t.idmap[extID] = id
	return id
}

// lookupChild returns LookupID(id) for a type ID referenced by the typedef for
// extID, which isn't a named type, and panics if id doesn't precede extID.
// Since the typedefs aren't validated when a TypeDefs is created, this keeps a
// malformed table from causing unbounded recursion.
func (t *TypeDefsMerger) lookupChild(id, extID uint32) uint32 {
	if id >= extID {
		panic(id)
	}
	return t.LookupID(id)
}

// OptionizeMissing returns typ with every union type with a missing member, i.e.,
// (missing|T) or (missing|T1|T2|...), replaced by the option type option(T)
// or option(T1|T2|...).  Since an option already represents an absent value,
// none members and option members of such a union are absorbed into the
// result, e.g., (missing|none|option(T1)|T2) becomes option(T1|T2).
// In addition, a null member of the type of a record field is treated like
// missing, i.e., a field of type (null|T), (missing|T), or (missing|null|T)
// becomes a field of type option(T), unless null is the only type left
// besides missing, in which case the field is left as is.
// OptionizeMissing descends into all complex types except named types,
// which are left as is (since a named type's name is bound to its type and
// named types may be recursive).
func (c *Context) OptionizeMissing(typ Type) Type {
	switch typ := typ.(type) {
	case *TypeRecord:
		var fields []Field
		for k, f := range typ.Fields {
			if t := c.optionizeNullField(c.OptionizeMissing(f.Type)); t != f.Type {
				if fields == nil {
					fields = slices.Clone(typ.Fields)
				}
				fields[k].Type = t
			}
		}
		if fields == nil {
			return typ
		}
		return c.MustLookupTypeRecord(fields)
	case *TypeArray:
		if t := c.OptionizeMissing(typ.Type); t != typ.Type {
			return c.LookupTypeArray(t)
		}
	case *TypeSet:
		if t := c.OptionizeMissing(typ.Type); t != typ.Type {
			return c.LookupTypeSet(t)
		}
	case *TypeMap:
		key, val := c.OptionizeMissing(typ.KeyType), c.OptionizeMissing(typ.ValType)
		if key != typ.KeyType || val != typ.ValType {
			return c.LookupTypeMap(key, val)
		}
	case *TypeError:
		if t := c.OptionizeMissing(typ.Type); t != typ.Type {
			return c.LookupTypeError(t)
		}
	case *TypeOption:
		if t := c.OptionizeMissing(typ.Type); t != typ.Type {
			if _, ok := t.(*TypeOption); ok {
				return t
			}
			return c.LookupTypeOption(t)
		}
	case *TypeFusion:
		if t := c.OptionizeMissing(typ.Type); t != typ.Type {
			return c.MustLookupTypeFusion(t)
		}
	case *TypeUnion:
		return c.optionizeUnion(typ)
	}
	return typ
}

// optionizeNullField returns typ, the optionized type of a record field,
// with a null member removed and the result made an option, i.e., (null|T)
// becomes option(T) and option(null|T) becomes option(T).  If null is the
// only type, typ is returned as is.
func (c *Context) optionizeNullField(typ Type) Type {
	var union *TypeUnion
	switch t := typ.(type) {
	case *TypeUnion:
		union = t
	case *TypeOption:
		if u, ok := t.Type.(*TypeUnion); ok {
			union = u
		}
	}
	if union == nil || !slices.Contains(union.Types, Type(TypeNull)) {
		return typ
	}
	var inner []Type
	for _, t := range union.Types {
		if t != TypeNull {
			inner = append(inner, t)
		}
	}
	switch len(inner) {
	case 0:
		return typ
	case 1:
		if _, ok := inner[0].(*TypeOption); ok {
			return inner[0]
		}
		return c.LookupTypeOption(inner[0])
	}
	return c.LookupTypeOption(c.MustLookupTypeUnion(inner))
}

func (c *Context) optionizeUnion(union *TypeUnion) Type {
	var types []Type
	changed := false
	for _, t := range union.Types {
		u := c.OptionizeMissing(t)
		changed = changed || u != t
		types = append(types, u)
	}
	if !slices.Contains(types, Type(TypeMissing)) {
		if !changed {
			return union
		}
		return c.MustLookupTypeUnion(types)
	}
	var inner []Type
	for _, t := range types {
		switch t := t.(type) {
		case *TypeOption:
			if u, ok := t.Type.(*TypeUnion); ok {
				inner = append(inner, u.Types...)
			} else {
				inner = append(inner, t.Type)
			}
		default:
			if t != TypeMissing && t != TypeNone {
				inner = append(inner, t)
			}
		}
	}
	inner = UniqueTypes(inner)
	switch len(inner) {
	case 0:
		// The union has only missing and none, so the best we can do is
		// leave it as is.
		return union
	case 1:
		return c.LookupTypeOption(inner[0])
	}
	return c.LookupTypeOption(c.MustLookupTypeUnion(inner))
}
