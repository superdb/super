package bsup_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

func TestProjectMetadata(t *testing.T) {
	var b bytes.Buffer
	w := bsup.NewColumnWriter(sio.NopCloser(&b))
	sctx := super.NewContext()
	supValues := []string{
		"{a:1,b:{c:4,d:0.7}}",
		"{a:2,b:{c:5,d:0.8}}",
		"{a:3,b:{c:6,d:0.9}}",
	}
	builder := vector.NewDynamicValueBuilder()
	for _, s := range supValues {
		builder.Write(sup.MustParseValue(sctx, s))
	}
	require.NoError(t, w.Push(builder.Build(sctx)))
	require.NoError(t, w.Close())
	bsupBytes := b.Bytes()

	fit := bsup.NewSeekable(super.NewContext(), bytes.NewReader(bsupBytes))
	frame, err := fit.Next()
	require.NoError(t, err)
	colFrame, ok := frame.(*bsup.ColFrame)
	require.Equal(t, true, ok)
	p := field.NewProjection(field.DottedList("b.d,a"))
	values := colFrame.ProjectMetadata(super.NewContext(), p)
	require.Len(t, values, 1)
	require.Equal(t, "{b:{d:{min:0.7,max:0.9}},a:{min:1,max:3}}", sup.FormatValue(values[0]))
}

func TestProjectMetadataForUnion(t *testing.T) {
	sctx := super.NewContext()
	supValues := []string{
		"{a:1::(int64|string),b?:2,c:3::(int64|null),d?:{e:4}}",
		`{a:"s"::(int64|string),b?:none::int64,c:null::(int64|null),d?:none::{e:int64}}`,
	}
	builder := vector.NewDynamicValueBuilder()
	for _, s := range supValues {
		builder.Write(sup.MustParseValue(sctx, s))
	}
	var b bytes.Buffer
	w := bsup.NewColumnWriter(sio.NopCloser(&b))
	require.NoError(t, w.Push(builder.Build(sctx)))
	require.NoError(t, w.Close())
	bsupBytes := b.Bytes()

	fit := bsup.NewSeekable(sctx, bytes.NewReader(bsupBytes))
	frame, err := fit.Next()
	require.NoError(t, err)
	colFrame, ok := frame.(*bsup.ColFrame)
	require.Equal(t, true, ok)
	p := field.NewProjection(field.DottedList("a,b,c,d"))
	values := colFrame.ProjectMetadata(super.NewContext(), p)
	require.Len(t, values, 1)
	require.Equal(t, `{a:{min:1,max:"s"},b:{min:2,max:2},c:{min:3,max:3},d:{e:{min:4,max:4}}}`, sup.FormatValue(values[0]))
}
