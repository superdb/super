package bsup_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/fuzz"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/bsupio"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

func FuzzBSUPRoundtripGen(f *testing.F) {
	f.Fuzz(func(t *testing.T, b []byte) {
		bytesReader := bytes.NewReader(b)
		sctx := super.NewContext()
		types := fuzz.GenTypes(bytesReader, sctx, 3)
		values := fuzz.GenValues(bytesReader, sctx, types)
		roundtrip(t, sctx, values)
	})
}

func FuzzBSUPRoundtripBytes(f *testing.F) {
	f.Fuzz(func(t *testing.T, b []byte) {
		sctx := super.NewContext()
		values, err := fuzz.ReadBSUP(t.Context(), sctx, b)
		if err != nil {
			t.Skipf("%v", err)
		}
		roundtrip(t, sctx, values)
	})
}

func roundtrip(t *testing.T, sctx *super.Context, valuesIn []super.Value) {
	var buf bytes.Buffer
	fuzz.WriteBSUP(t, sctx, valuesIn, &buf)
	valuesOut, err := fuzz.ReadBSUP(t.Context(), sctx, buf.Bytes())
	require.NoError(t, err)
	fuzz.CompareValues(t, valuesIn, valuesOut)
}

func TestBSUPBatchBug(t *testing.T) {
	var b bytes.Buffer
	w := bsup.NewColumnWriter(sio.NopCloser(&b))
	sctx := super.NewContext()
	v1, err := sup.ParseValue(sctx, `{a: [1,2,3]}`)
	require.NoError(t, err)
	val2, err := sup.ParseValue(sctx, `{a:[4,5]}`)
	require.NoError(t, err)
	err = w.Push(valToVec(sctx, v1))
	require.NoError(t, err)
	err = w.Push(valToVec(sctx, val2))
	err = w.Close()
	require.NoError(t, err)
	p, err := bsupio.NewReader(t.Context(), sctx, bytes.NewReader(b.Bytes()), nil, nil, 1)
	require.NoError(t, err)
	defer p.Pull(true)
	r := sbuf.PullerReader(sbuf.NewMaterializer(p))
	val, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, "{a:[1,2,3]}", sup.String(val))
	val, err = r.Read()
	require.NoError(t, err)
	require.Equal(t, "{a:[4,5]}", sup.String(val))
}

func TestColumnWriterControlFrames(t *testing.T) {
	var b bytes.Buffer
	w := bsup.NewColumnWriter(sio.NopCloser(&b))
	sctx := super.NewContext()
	for _, s := range []string{`{a:1}`, `{a:2}`} {
		val, err := sup.ParseValue(sctx, s)
		require.NoError(t, err)
		require.NoError(t, w.Push(valToVec(sctx, val)))
		// WriteControl ends the column frame and writes a control frame.
		require.NoError(t, w.WriteControl(super.NewString("ctrl")))
	}
	require.NoError(t, w.Close())
	// The super frame size must include the control frames so the
	// super footer's back pointer leads to the start of the file.
	seekable := bsup.NewSeekable(sctx, bytes.NewReader(b.Bytes()))
	typ, err := seekable.FusedType(sctx)
	require.NoError(t, err)
	require.Equal(t, "{a:int64}", sup.FormatType(typ))
	// The zone maps' frame offsets must locate the column frames.
	offs, ok, err := seekable.FilterFrames(sctx, nil, func(super.Value) bool { return false })
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, offs, 2)
	for _, off := range offs {
		require.Equal(t, bsup.ColumnMagic, string(b.Bytes()[off:off+4]))
	}
}

func valToVec(sctx *super.Context, val super.Value) vector.Any {
	b := vector.NewDynamicValueBuilder()
	b.Write(val)
	return b.Build(sctx)
}
