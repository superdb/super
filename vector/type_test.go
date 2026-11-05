package vector_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

type testLoader struct {
	sctx *super.Context
	defs *super.TypeDefs
	ids  []uint32
	tmap *super.TypeIDMap
}

func (l *testLoader) Load() (*super.TypeIDMap, []uint32) {
	if l.tmap == nil {
		l.tmap = super.NewTypeIDMap(l.sctx, l.defs)
	}
	return l.tmap, l.ids
}

func TestTypeValueIDs(t *testing.T) {
	// Encode types from another context in a foreign TypeDefs table.
	ext := super.NewContext()
	ext.MustLookupTypeRecord(nil) // shift ext's IDs from sctx's
	rec := ext.MustLookupTypeRecord([]super.Field{super.NewField("a", super.TypeInt64)})
	arr := ext.LookupTypeArray(rec)
	enc := super.NewTypeDefsEncoder()
	recID, arrID := enc.LookupType(rec), enc.LookupType(arr)
	loader := &testLoader{
		defs: &enc.TypeDefs,
		ids:  []uint32{recID, arrID, recID, super.IDString},
	}
	sctx := super.NewContext()
	loader.sctx = sctx
	vec := vector.NewTypeValueWithLoader(sctx, loader)
	require.True(t, vec.IsForeign())
	// The length doesn't require mapping the IDs.
	assert.Equal(t, uint32(4), vec.Len())
	assert.Equal(t, 0, sctx.TypeDefs().NTypes())
	// The IDs are mapped to type IDs in sctx, but the TypeValue stays
	// foreign.
	ids := vec.IDs()
	require.True(t, vec.IsForeign())
	require.Len(t, ids, 4)
	assert.Equal(t, ids[0], ids[2])
	assert.Equal(t, uint32(super.IDString), ids[3])
	for k, want := range []string{"{a:int64}", "[{a:int64}]", "{a:int64}", "string"} {
		typ, err := sctx.LookupType(int(ids[k]))
		require.NoError(t, err)
		assert.Same(t, typ, vec.Value(uint32(k)))
		assert.Equal(t, want, sup.FormatType(typ))
	}
	// The loader's table is still used for TypeIDs.
	defs, foreign := vec.TypeIDs()
	assert.Same(t, &enc.TypeDefs, defs)
	assert.Equal(t, loader.ids, foreign)
}

func TestTypeValueAppendForeign(t *testing.T) {
	// A type from another context is translated into sctx.
	ext := super.NewContext()
	ext.MustLookupTypeRecord(nil)
	rec := ext.MustLookupTypeRecord([]super.Field{super.NewField("a", super.TypeInt64)})
	sctx := super.NewContext()
	vec := vector.NewTypeValue(sctx, []super.Type{rec, super.TypeString})
	local := sctx.MustLookupTypeRecord([]super.Field{super.NewField("a", super.TypeInt64)})
	assert.Equal(t, []uint32{uint32(super.TypeID(local)), super.IDString}, vec.IDs())
	assert.Same(t, local, vec.Value(0))
}

func TestTypeValueTypeIDsFromContext(t *testing.T) {
	// The TypeIDs of type values in sctx are their type IDs in a snapshot of
	// sctx's typedefs, which can be mapped or merged into another table.
	sctx := super.NewContext()
	rec := sctx.MustLookupTypeRecord([]super.Field{super.NewField("a", super.TypeInt64)})
	types := []super.Type{rec, sctx.LookupTypeArray(rec), super.TypeString}
	vec := vector.NewTypeValue(sctx, types)
	defs, ids := vec.TypeIDs()
	assert.Equal(t, vec.IDs(), ids)
	// More types entered in sctx don't affect the snapshot.
	n := defs.NTypes()
	sctx.LookupTypeSet(rec)
	assert.Equal(t, n, defs.NTypes())
	merged := super.NewTypeDefsBuilder()
	merger := super.NewTypeDefsMerger(merged, defs)
	mapper := super.NewTypeDefsMapper(super.NewContext(), &merged.TypeDefs)
	for k, id := range ids {
		out := mapper.LookupType(merger.LookupID(id))
		require.NotNil(t, out)
		assert.Equal(t, sup.FormatType(types[k]), sup.FormatType(out))
	}
}

func TestTypeValueMapOnDemand(t *testing.T) {
	// Only the types of the slots that are used are created in sctx.
	ext := super.NewContext()
	rec := ext.MustLookupTypeRecord([]super.Field{super.NewField("a", super.TypeInt64)})
	arr := ext.LookupTypeArray(ext.MustLookupTypeRecord([]super.Field{super.NewField("b", super.TypeString)}))
	enc := super.NewTypeDefsEncoder()
	loader := &testLoader{
		defs: &enc.TypeDefs,
		ids:  []uint32{enc.LookupType(arr), enc.LookupType(rec), super.IDString},
	}
	sctx := super.NewContext()
	loader.sctx = sctx
	vec := vector.NewTypeValueWithLoader(sctx, loader)
	// A pick shares the mapping and maps only its slots.
	pick := vec.Pick([]uint32{1, 2})
	require.True(t, pick.IsForeign())
	assert.Equal(t, uint32(2), pick.Len())
	assert.Equal(t, 0, sctx.TypeDefs().NTypes())
	assert.Equal(t, "{a:int64}", sup.FormatType(pick.Value(0)))
	assert.Equal(t, "string", sup.FormatType(pick.Value(1)))
	assert.Equal(t, 1, sctx.TypeDefs().NTypes())
	// The original uses the same mapping for the record and maps the array
	// (and its element record) when it's used.
	assert.Same(t, pick.Value(0), vec.Value(1))
	assert.Equal(t, 1, sctx.TypeDefs().NTypes())
	assert.Equal(t, "[{b:string}]", sup.FormatType(vec.Value(0)))
	assert.Equal(t, 3, sctx.TypeDefs().NTypes())
	// The pick's TypeIDs are foreign IDs in the foreign table.
	defs, ids := pick.TypeIDs()
	assert.Same(t, &enc.TypeDefs, defs)
	assert.Equal(t, loader.ids[1:], ids)
}

func TestTypeValueConcurrent(t *testing.T) {
	// The views of a foreign TypeValue may map their type values
	// concurrently.
	ext := super.NewContext()
	enc := super.NewTypeDefsEncoder()
	var ids []uint32
	for k := range 50 {
		name := string(rune('a' + k%26))
		ids = append(ids, enc.LookupType(ext.LookupTypeArray(ext.MustLookupTypeRecord([]super.Field{super.NewField(name, super.TypeInt64)}))))
	}
	sctx := super.NewContext()
	vec := vector.NewTypeValueWithLoader(sctx, &testLoader{sctx: sctx, defs: &enc.TypeDefs, ids: ids})
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			index := []uint32{uint32(g), uint32(49 - g), 25}
			pick := vec.Pick(index)
			for k, slot := range index {
				assert.Same(t, vec.Value(slot), pick.Value(uint32(k)))
			}
		})
	}
	wg.Wait()
}
