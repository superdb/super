package bsup_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/bsup/loader"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

func values(vec vector.Any) []string {
	var out []string
	var b scode.Builder
	for slot := range vec.Len() {
		out = append(out, sup.String(vector.ValueAt(&b, vec, slot)))
	}
	return out
}

// TestEncodeShapes encodes vectors of shapes the dynamic builder of a
// ColumnWriter doesn't produce and checks that they load.
func TestEncodeShapes(t *testing.T) {
	sctx := super.NewContext()
	rec := func(name string, vec vector.Any) vector.Any {
		typ := sctx.MustLookupTypeRecord([]super.Field{super.NewField(name, vec.Type())})
		return vector.NewRecord(typ, []vector.Any{vec}, vec.Len())
	}
	named, err := sctx.LookupTypeNamed("N", super.TypeInt64)
	require.NoError(t, err)
	namedRec, err := sctx.LookupTypeNamed("R", sctx.MustLookupTypeRecord([]super.Field{super.NewField("x", super.TypeInt64)}))
	require.NoError(t, err)
	ints := vector.NewInt(super.TypeInt64, []int64{1, 2, 3})
	one := func(vec vector.Any) *vector.Dynamic {
		return vector.NewDynamic(make([]uint32, vec.Len()), []vector.Any{vec})
	}
	cases := map[string]*vector.Dynamic{
		"named const":           one(rec("a", vector.NewNamed(named, vector.NewConstInt(super.TypeInt64, 7, 3)))),
		"named dict":            one(rec("a", vector.NewNamed(named, vector.NewDict(vector.NewInt(super.TypeInt64, []int64{5, 6}), []byte{0, 1, 0}, []uint32{2, 1})))),
		"const record":          one(vector.NewConstFromValue(sctx, sup.MustParseValue(sctx, `{x:1,y:"a",z:[1,2]}`), 3)),
		"named empty in record": one(rec("a", vector.NewNamed(namedRec, vector.NewEmpty(namedRec.Type)))),
		"error const":           one(rec("e", vector.NewError(sctx.LookupTypeError(super.TypeString), vector.NewConstString("bad", 3)))),
		"dynamic with empty":    vector.NewDynamic([]uint32{0, 0, 0}, []vector.Any{ints, vector.NewEmpty(super.TypeString)}),
		"dynamic with const and empty record": vector.NewDynamic([]uint32{0, 1, 0, 1}, []vector.Any{
			vector.NewConstString("s", 2),
			vector.NewConstFromValue(sctx, sup.MustParseValue(sctx, "{x:1}"), 2),
			vector.NewEmpty(namedRec),
		}),
	}
	for name, vec := range cases {
		t.Run(name, func(t *testing.T) {
			b, err := bsup.EncodeFrame(vec)
			require.NoError(t, err)
			frame, err := bsup.NewSeekable(sctx, bytes.NewReader(b)).Next()
			require.NoError(t, err)
			l, err := loader.NewFrameLoader(sctx, frame.(*bsup.ColFrame))
			require.NoError(t, err)
			assert.Equal(t, values(vec), values(l.Load(nil)))
		})
	}
}

// nested returns a SUP array of int64 nested n deep.
func nested(n int) string {
	return strings.Repeat("[", n) + "1" + strings.Repeat("]", n)
}

func TestEncodeMaxDepth(t *testing.T) {
	// A value whose vectors are nested bsup.MaxLocatorDepth deep (the
	// arrays and their int64 values) is written and read but one nested
	// more deeply isn't written.
	sctx := super.NewContext()
	write := func(s string) ([]byte, error) {
		var buf bytes.Buffer
		w := bsup.NewColumnWriter(sio.NopCloser(&buf))
		b := vector.NewDynamicValueBuilder()
		b.Write(sup.MustParseValue(sctx, s))
		if err := w.Push(b.Build(sctx)); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	s := nested(bsup.MaxLocatorDepth - 1)
	b, err := write(s)
	require.NoError(t, err)
	frame, err := bsup.NewSeekable(sctx, bytes.NewReader(b)).Next()
	require.NoError(t, err)
	l, err := loader.NewFrameLoader(sctx, frame.(*bsup.ColFrame))
	require.NoError(t, err)
	assert.Equal(t, []string{s}, values(l.Load(nil)))
	_, err = write(nested(bsup.MaxLocatorDepth))
	assert.ErrorContains(t, err, "BSUP vectors nested more than 10000 deep")
}
