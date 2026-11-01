package loader_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/bsup/loader"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

// load writes the values in sups as a BSUP frame and returns the vector
// loaded with projection.
func load(t *testing.T, sctx *super.Context, projection field.Projection, sups ...string) vector.Any {
	b := vector.NewDynamicValueBuilder()
	for _, s := range sups {
		val, err := sup.ParseValue(sctx, s)
		require.NoError(t, err)
		b.Write(val)
	}
	var buf bytes.Buffer
	w := bsup.NewColumnWriter(sio.NopCloser(&buf))
	require.NoError(t, w.Push(b.Build(sctx)))
	require.NoError(t, w.Close())
	frame, err := bsup.NewSeekable(sctx, bytes.NewReader(buf.Bytes())).Next()
	require.NoError(t, err)
	return loader.NewFrameLoader(sctx, frame.(*bsup.ColFrame)).Load(projection)
}

func requireTypeOnly(t *testing.T, vec vector.Any) {
	_, ok := vec.(*vector.TypeOnly)
	require.True(t, ok, "%T is not a TypeOnly vector", vec)
	require.Panics(t, func() { vec.Serialize(scode.NewBuilder(), 0) })
}

func TestTypeOnlyProjection(t *testing.T) {
	sctx := super.NewContext()
	typesOnly := field.NewProjectionMaybe(true, nil, []field.Path{{}})
	vec := load(t, sctx, typesOnly,
		`{a:1,b:{c:"x"},e:[1,2],u:1::(int64|string)}`,
		`{a:2,b:{c:"y"},e:[3],u:"s"::(int64|string)}`)
	require.Equal(t, `{a:int64,b:{c:string},e:[int64],u:int64|string}`, sup.FormatType(vec.Type()))
	require.Equal(t, uint32(2), vec.Len())
	rec := vec.(*vector.Record)
	requireTypeOnly(t, rec.Fields[0])
	requireTypeOnly(t, rec.Fields[1].(*vector.Record).Fields[0])
	requireTypeOnly(t, rec.Fields[2])
	require.Equal(t, uint32(2), rec.Fields[2].Len())
	// The union tags are loaded but not the values.
	union := rec.Fields[3].(*vector.Union)
	require.Equal(t, []uint32{0, 1}, union.Tags())
	for _, vec := range union.Values() {
		requireTypeOnly(t, vec)
	}
}

func TestTypeOnlyProjectionWithData(t *testing.T) {
	sctx := super.NewContext()
	// The type of this plus the data for b.c.
	projection := field.NewProjectionMaybe(true, []field.Path{{"b", "c"}}, []field.Path{{}, {"b"}})
	vec := load(t, sctx, projection, `{a:1,b:{c:"x",d:2}}`)
	require.Equal(t, `{a:int64,b:{c:string,d:int64}}`, sup.FormatType(vec.Type()))
	rec := vec.(*vector.Record)
	requireTypeOnly(t, rec.Fields[0])
	b := rec.Fields[1].(*vector.Record)
	require.Equal(t, `"x"`, sup.FormatValue(vector.ValueAt(nil, b.Fields[0], 0)))
	requireTypeOnly(t, b.Fields[1])
}

func TestTypeOnlyProjectionFusion(t *testing.T) {
	sctx := super.NewContext()
	typesOnly := field.NewProjectionMaybe(true, nil, []field.Path{{}})
	vec := load(t, sctx, typesOnly,
		`fusion({x?:1,y?:none::string},<{x:int64}>)`,
		`fusion({x?:none::int64,y?:"foo"},<{y:string}>)`)
	fusion := vec.(*vector.Fusion)
	require.Equal(t, `fusion({x?:int64,y?:string})`, sup.FormatType(fusion.Type()))
	// The subtypes are loaded but the values are not.
	require.Equal(t, `{x:int64}`, sup.FormatType(fusion.Subtypes.Value(0)))
	require.Equal(t, `{y:string}`, sup.FormatType(fusion.Subtypes.Value(1)))
	require.Panics(t, func() { fusion.Values.Serialize(scode.NewBuilder(), 0) })
}
