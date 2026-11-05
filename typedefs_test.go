package super_test

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/sio/supio"
	"github.com/superdb/super/sup"
)

func TestTypeDefsRoundTrip(t *testing.T) {
	sctx := super.NewContext()
	var types []super.Type
	for _, s := range []string{
		"{a:int64,b:[string],c:set[ip]}",
		"int64|string",
		"map{string:{x:float64}}",
		"error(string)",
		"enum(a,b)",
		"decimal(10,2)",
		"option(int64)",
		"fusion(int64|string)",
	} {
		typ, err := sup.ParseType(sctx, s)
		require.NoError(t, err, s)
		types = append(types, typ)
	}
	// Named types, including a recursive one.
	r := supio.NewReader(sctx, strings.NewReader(`
type foo={x:int64}
type tree={l:tree|null,v:int64}
{x:1}::foo
{l:{l:null,v:1}::tree,v:2}::tree
`))
	for {
		val, err := r.Read()
		require.NoError(t, err)
		if val == nil {
			break
		}
		types = append(types, val.Type())
	}
	require.Len(t, types, 10)
	enc := super.NewTypeDefsEncoder()
	var ids []uint32
	for _, typ := range types {
		ids = append(ids, enc.LookupType(typ))
	}
	// Types are interned.
	for k, typ := range types {
		assert.Equal(t, ids[k], enc.LookupType(typ))
	}
	defs, ok := super.NewTypeDefsFromBytes(enc.Bytes())
	require.True(t, ok)
	assert.Equal(t, enc.NTypes(), defs.NTypes())
	for id := uint32(super.IDTypeComplex); id < uint32(super.IDTypeComplex+defs.NTypes()); id++ {
		assert.Equal(t, enc.Serialization(id), defs.Serialization(id))
	}
	// The types map to the same types in a new context.
	mapper := super.NewTypeDefsMapper(super.NewContext(), defs)
	for k, typ := range types {
		out := mapper.LookupType(ids[k])
		require.NotNil(t, out)
		assert.Equal(t, sup.FormatType(typ), sup.FormatType(out))
	}
	// An encoder's TypeDefs reflects types entered later.
	view := &enc.TypeDefs
	n := view.NTypes()
	enc.LookupType(sctx.LookupTypeArray(types[0]))
	assert.Equal(t, n+1, view.NTypes())
	// The merger copies the types by ID into another builder.
	merged := super.NewTypeDefsBuilder()
	merger := super.NewTypeDefsMerger(merged, defs)
	mapper = super.NewTypeDefsMapper(super.NewContext(), &merged.TypeDefs)
	for k, typ := range types {
		out := mapper.LookupType(merger.LookupID(ids[k]))
		require.NotNil(t, out)
		assert.Equal(t, sup.FormatType(typ), sup.FormatType(out))
	}
	// A Context's builder holds its types by their IDs.
	defs, ok = super.NewTypeDefsFromBytes(sctx.TypeDefs().Bytes())
	require.True(t, ok)
	mapper = super.NewTypeDefsMapper(super.NewContext(), defs)
	for _, typ := range types {
		out := mapper.LookupType(uint32(super.TypeID(typ)))
		require.NotNil(t, out)
		assert.Equal(t, sup.FormatType(typ), sup.FormatType(out))
	}
}

func TestTypeDefsEmpty(t *testing.T) {
	builder := super.NewTypeDefsBuilder()
	assert.Empty(t, builder.Bytes())
	defs, ok := super.NewTypeDefsFromBytes(nil)
	require.True(t, ok)
	assert.Equal(t, 0, defs.NTypes())
	assert.Nil(t, defs.Serialization(super.IDTypeComplex))
	assert.Nil(t, super.NewTypeDefsMapper(super.NewContext(), defs).LookupType(super.IDTypeComplex))
}

func TestTypeDefsMalformed(t *testing.T) {
	array := func(id uint32) []byte {
		return binary.AppendUvarint([]byte{super.TypeDefArray}, uint64(id))
	}
	encode := func(offsets []uint64, section []byte) []byte {
		b := binary.AppendUvarint(nil, uint64(len(offsets)))
		for _, off := range offsets {
			b = binary.AppendUvarint(b, off)
		}
		return append(b, section...)
	}
	// Valid: [int64] then [[int64]].
	section := append(array(super.IDInt64), array(super.IDTypeComplex)...)
	_, ok := super.NewTypeDefsFromBytes(encode([]uint64{0, 2}, section))
	assert.True(t, ok)
	for name, b := range map[string][]byte{
		"truncated count":     {0x80},
		"count too large":     encode([]uint64{0, 2, 4}, nil),
		"first offset not 0":  encode([]uint64{1, 2}, section),
		"offsets not ordered": encode([]uint64{0, 0}, section),
		"offset past end":     encode([]uint64{0, 4}, section),
	} {
		_, ok := super.NewTypeDefsFromBytes(b)
		assert.False(t, ok, name)
	}
	// A typedef that refers to itself or a later typedef is detected on
	// lookup rather than causing unbounded recursion.
	for name, section := range map[string][]byte{
		"self reference":    array(super.IDTypeComplex),
		"forward reference": append(array(super.IDTypeComplex+1), array(super.IDInt64)...),
		"bad typedef code":  {0xff},
	} {
		n := 1
		if name == "forward reference" {
			n = 2
		}
		offsets := []uint64{0, 2}[:n]
		defs, ok := super.NewTypeDefsFromBytes(encode(offsets, section))
		require.True(t, ok, name)
		mapper := super.NewTypeDefsMapper(super.NewContext(), defs)
		assert.Nil(t, mapper.LookupType(super.IDTypeComplex), name)
	}
}

func TestContextForeignTypes(t *testing.T) {
	// A Context translates a type from another context into itself.
	other := super.NewContext()
	other.MustLookupTypeRecord(nil) // shift other's IDs from sctx's
	union := other.MustLookupTypeUnion([]super.Type{super.TypeInt64, super.TypeString})
	sctx := super.NewContext()
	rec := sctx.MustLookupTypeRecord([]super.Field{super.NewField("u", union), super.NewField("a", other.LookupTypeArray(union))})
	// The record's field types are types of sctx.
	for _, f := range rec.Fields {
		local, err := sctx.LookupType(super.TypeID(f.Type))
		require.NoError(t, err)
		assert.Same(t, local, f.Type)
	}
	defs, ok := super.NewTypeDefsFromBytes(sctx.TypeDefs().Bytes())
	require.True(t, ok)
	out := super.NewTypeDefsMapper(super.NewContext(), defs).LookupType(uint32(super.TypeID(rec)))
	require.NotNil(t, out)
	assert.Equal(t, "{u:int64|string,a:[int64|string]}", sup.FormatType(out))
}

func TestContextForeignNamedTypes(t *testing.T) {
	ext := super.NewContext()
	ext.MustLookupTypeRecord(nil) // shift ext's IDs from sctx's
	foo, err := ext.LookupTypeNamed("foo", super.TypeInt64)
	require.NoError(t, err)
	// A foreign named type is translated, binding its name in sctx, so the
	// name can then be looked up and bound to the same type.
	sctx := super.NewContext()
	arr := sctx.LookupTypeArray(foo)
	local := sctx.LookupByName("foo")
	require.NotNil(t, local)
	assert.Same(t, local, arr.Type)
	same, err := sctx.LookupTypeNamed("foo", super.TypeInt64)
	require.NoError(t, err)
	assert.Same(t, local, same)
	// A named type with a foreign inner type is bound to its translation.
	rec := ext.MustLookupTypeRecord([]super.Field{super.NewField("f", foo)})
	bar, err := sctx.LookupTypeNamed("bar", rec)
	require.NoError(t, err)
	assert.Same(t, sctx.MustLookupTypeRecord([]super.Field{super.NewField("f", local)}), bar.Type)
	// A foreign named type whose name is bound to a different type in sctx
	// can't be translated.
	other := super.NewContext()
	_, err = other.LookupTypeNamed("foo", super.TypeString)
	require.NoError(t, err)
	assert.Panics(t, func() { other.LookupTypeArray(foo) })
	_, err = other.LookupTypeRecord([]super.Field{super.NewField("f", foo)})
	assert.Error(t, err)
	_, err = other.LookupTypeNamed("baz", foo)
	assert.Error(t, err)
}

func TestTranslateType(t *testing.T) {
	ext := super.NewContext()
	ext.MustLookupTypeRecord(nil) // shift ext's IDs from sctx's
	r := supio.NewReader(ext, strings.NewReader(`
type tree={l:tree|null,v:int64}
{a:[1],u:1::(int64|string),t:{l:null,v:1}::tree}
`))
	val, err := r.Read()
	require.NoError(t, err)
	sctx := super.NewContext()
	typ, err := sctx.TranslateType(val.Type())
	require.NoError(t, err)
	assert.Equal(t, sup.FormatType(val.Type()), sup.FormatType(typ))
	// The translated type is a type of sctx, which translates to itself.
	again, err := sctx.TranslateType(typ)
	require.NoError(t, err)
	assert.Same(t, typ, again)
	// Translating it again finds the existing types, including the
	// recursive named type.
	again, err = sctx.TranslateType(val.Type())
	require.NoError(t, err)
	assert.Same(t, typ, again)
	// A type value is the same in both contexts and decodes to the
	// translated type.
	tv := sctx.LookupTypeValue(val.Type())
	assert.Equal(t, ext.LookupTypeValue(val.Type()).Bytes(), tv.Bytes())
	decoded, err := sctx.LookupByValue(tv.Bytes())
	require.NoError(t, err)
	assert.Same(t, typ, decoded)
}

func TestTranslateTypeRedefined(t *testing.T) {
	// A named type can't be translated into a context where its name is
	// bound to a different type.
	ext := super.NewContext()
	foo, err := ext.LookupTypeNamed("foo", ext.MustLookupTypeRecord([]super.Field{super.NewField("x", super.TypeInt64)}))
	require.NoError(t, err)
	sctx := super.NewContext()
	_, err = sctx.LookupTypeNamed("foo", sctx.MustLookupTypeRecord([]super.Field{super.NewField("y", super.TypeString)}))
	require.NoError(t, err)
	_, err = sctx.TranslateType(foo)
	assert.Error(t, err)
	_, err = sctx.LookupByValue(super.EncodeTypeValue(foo))
	assert.Error(t, err)
	// With the same binding, the existing named type is used.
	same, err := super.NewContext().TranslateType(foo)
	require.NoError(t, err)
	other := super.NewContext()
	bar, err := other.LookupTypeNamed("foo", other.MustLookupTypeRecord([]super.Field{super.NewField("x", super.TypeInt64)}))
	require.NoError(t, err)
	got, err := other.TranslateType(foo)
	require.NoError(t, err)
	assert.Same(t, bar, got)
	assert.Equal(t, "foo", same.(*super.TypeNamed).Name)
}

func TestTypeDefsNamedCycle(t *testing.T) {
	// A cycle of named types with no other type in between, e.g., foo=foo
	// or foo=bar,bar=foo, is malformed since the named types would have no
	// underlying type.
	named := func(name string, inner uint32) []byte {
		b := binary.AppendUvarint([]byte{super.TypeDefNamed}, uint64(len(name)))
		return super.AppendFixedID(append(b, name...), inner)
	}
	encode := func(typedefs ...[]byte) []byte {
		b := binary.AppendUvarint(nil, uint64(len(typedefs)))
		var off int
		for _, td := range typedefs {
			b = binary.AppendUvarint(b, uint64(off))
			off += len(td)
		}
		for _, td := range typedefs {
			b = append(b, td...)
		}
		return b
	}
	for name, b := range map[string][]byte{
		"self":   encode(named("foo", super.IDTypeComplex)),
		"mutual": encode(named("foo", super.IDTypeComplex+1), named("bar", super.IDTypeComplex)),
	} {
		defs, ok := super.NewTypeDefsFromBytes(b)
		require.True(t, ok, name)
		mapper := super.NewTypeDefsMapper(super.NewContext(), defs)
		assert.Nil(t, mapper.LookupType(super.IDTypeComplex), name)
		merger := super.NewTypeDefsMerger(super.NewTypeDefsBuilder(), defs)
		assert.Panics(t, func() { merger.LookupID(super.IDTypeComplex) }, name)
	}
	// The same holds for a type value.
	tv := binary.AppendUvarint([]byte{super.TypeValueNameDef}, 3)
	tv = append(tv, "foo"...)
	tv = binary.AppendUvarint(append(tv, super.TypeValueNameRef), 3)
	tv = append(tv, "foo"...)
	_, err := super.NewContext().LookupByValue(tv)
	assert.Error(t, err)
	// A named type bound directly to another named type is fine.
	sctx := super.NewContext()
	foo, err := sctx.LookupTypeNamed("foo", super.TypeInt64)
	require.NoError(t, err)
	bar, err := sctx.LookupTypeNamed("bar", foo)
	require.NoError(t, err)
	assert.Same(t, foo, bar.Type)
}

func TestTypeDefsNamedBindingCheck(t *testing.T) {
	// tree returns the recursive type tree={l:tree|null,v:int64} in sctx.
	tree := func(sctx *super.Context) super.Type {
		named, patch, err := sctx.DeclareTypeNamed("tree")
		require.NoError(t, err)
		rec := sctx.MustLookupTypeRecord([]super.Field{
			super.NewField("l", sctx.MustLookupTypeUnion([]super.Type{named, super.TypeNull})),
			super.NewField("v", super.TypeInt64),
		})
		require.NoError(t, sctx.BindTypeNamed(patch, named, rec))
		return named
	}
	foo := func(inner super.Type) super.Type {
		sctx := super.NewContext()
		typ, err := sctx.LookupTypeNamed("foo", inner)
		require.NoError(t, err)
		return sctx.LookupTypeArray(typ)
	}
	// An encoder finds a named type from another context with the same
	// binding, including a recursive one, and panics if the binding differs.
	enc := super.NewTypeDefsEncoder()
	fooID := enc.LookupType(foo(super.TypeInt64))
	treeID := enc.LookupType(tree(super.NewContext()))
	assert.Equal(t, fooID, enc.LookupType(foo(super.TypeInt64)))
	assert.Equal(t, treeID, enc.LookupType(tree(super.NewContext())))
	assert.PanicsWithError(t, `type "foo" redefined`, func() { enc.LookupType(foo(super.TypeString)) })
	// Likewise, a merger finds a named type in the target with the same
	// binding and panics if the binding differs.
	merged := super.NewTypeDefsBuilder()
	merger := super.NewTypeDefsMerger(merged, &enc.TypeDefs)
	mergedFoo, mergedTree := merger.LookupID(fooID), merger.LookupID(treeID)
	same := super.NewTypeDefsEncoder()
	ids := []uint32{same.LookupType(tree(super.NewContext())), same.LookupType(foo(super.TypeInt64))}
	merger = super.NewTypeDefsMerger(merged, &same.TypeDefs)
	assert.Equal(t, mergedTree, merger.LookupID(ids[0]))
	assert.Equal(t, mergedFoo, merger.LookupID(ids[1]))
	other := super.NewTypeDefsEncoder()
	id := other.LookupType(foo(super.TypeString))
	merger = super.NewTypeDefsMerger(merged, &other.TypeDefs)
	assert.PanicsWithError(t, `type "foo" redefined`, func() { merger.LookupID(id) })
}
