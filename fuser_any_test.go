package super_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/sup"
)

func TestFuserAny(t *testing.T) {
	cases := []struct {
		types        []string
		super, fused string
	}{
		{[]string{"any", "int64"}, "any", "fusion(all)"},
		{[]string{"any", "{a:int64}", "[string]"}, "any", "fusion(all)"},
		{[]string{"any", "null"}, "any", "fusion(all)"},
		// any absorbs everything but missing.
		{[]string{"any", "missing"}, "missing|any", "fusion(all)"},
		{[]string{"any", "int64|missing"}, "missing|any", "fusion(all)"},
		{[]string{"{a:any}", "{a:int64}"}, "{a:any}", "{a:fusion(all)}"},
		{[]string{"{a:any}", "{b:int64}"}, "{a:missing|any,b:int64|missing}", "{a:fusion(all),b:fusion(int64|missing)}"},
		{[]string{"[any]", "[int64]"}, "[any]", "fusion([fusion(all)])"},
		{[]string{"{a:any}|string", "string"}, "string|{a:any}", "fusion(string|{a:fusion(all)})"},
		// A fused type with fusion(all) normalizes to any in the supertype.
		{[]string{"fusion(all)", "int64"}, "any", "fusion(all)"},
		{[]string{"{a:fusion(all)}", "{a:string}"}, "{a:any}", "{a:fusion(all)}"},
	}
	for _, c := range cases {
		t.Run(c.super, func(t *testing.T) {
			sctx := super.NewContext()
			fuser := super.NewFuser(sctx)
			for _, s := range c.types {
				typ, err := sup.ParseType(sctx, s)
				require.NoError(t, err)
				fuser.Fuse(typ)
			}
			require.Equal(t, c.super, sup.FormatType(fuser.SuperType()))
			require.Equal(t, c.fused, sup.FormatType(fuser.FusedType()))
		})
	}
}

func TestAnyToFusion(t *testing.T) {
	cases := []struct{ in, out string }{
		{"any", "fusion(all)"},
		{"int64", "int64"},
		{"{a:any,b:[any],c:int64}", "{a:fusion(all),b:[fusion(all)],c:int64}"},
		{"any|missing", "fusion(all)"},
		{"{a:any}|string", "string|{a:fusion(all)}"},
		{"any|string", "fusion(all)"},
		{"map{string:any}", "map{string:fusion(all)}"},
	}
	for _, c := range cases {
		sctx := super.NewContext()
		typ, err := sup.ParseType(sctx, c.in)
		require.NoError(t, err)
		require.Equal(t, c.out, sup.FormatType(sctx.AnyToFusion(typ)), c.in)
	}
}
