package super_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/sam/expr/function"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

func TestFuserSamePrimitiveTypeTwice(t *testing.T) {
	s := super.NewFuser(super.NewContext())
	typ := super.TypeInt64
	s.Fuse(typ)
	s.Fuse(typ)
	if sType := s.SuperType(); sType != typ {
		t.Fatalf("expected %s, got %s", sup.FormatType(typ), sup.FormatType(sType))
	}
}

// TestFusedTypeRoundTrip checks that the supertype of a set of values has
// no fusion types and that each value upcasts to the fused type and defuses
// back to itself.
func TestFusedTypeRoundTrip(t *testing.T) {
	cases := [][]string{
		{`1`, `"a"`},
		{`1`, `"a"`, `1::(int64|string)`},
		{`1::(int64|string)`, `2::(int64|string)`},
		{`1::(int64|null)`, `null`},
		{`{a:1}`, `{a:"x"}`},
		{`{a:1}`, `{b:"x"}`},
		{`{a:1}`, `{a:1,b:2}`},
		{`{b:1,a:2}`, `{a:1,b:2}`},
		{`{a:1}`, `[1]`, `{b:2}`},
		{`{a:1::(int64|string)}`, `{a:"x"}`},
		{`{a:[1]}`, `{a:["x"]}`, `{a:1}`},
		{`[1]`, `["a"]`},
		{`[1,"a"]`, `[2]`},
		{`[{a:1}]`, `[{b:2}]`},
		{`1::(int64|string)`, `{a:1}`},
		{`[1]::([int64]|string)`, `["x"]`},
	}
	for _, c := range cases {
		t.Run(c[0], func(t *testing.T) {
			sctx := super.NewContext()
			fuser := super.NewFuser(sctx)
			var vals []super.Value
			for _, s := range c {
				val, err := sup.ParseValue(sctx, s)
				require.NoError(t, err)
				vals = append(vals, val)
				fuser.Fuse(val.Type())
			}
			require.NotContains(t, sup.FormatType(fuser.SuperType()), "fusion", "supertype has fusion")
			fused := fuser.FusedType()
			upcast := function.NewUpcast(sctx)
			defuse := expr.NewSuperDefuse(sctx)
			for _, val := range vals {
				up, ok := upcast.Cast(val, fused)
				require.True(t, ok, "upcast %s to %s", sup.FormatValue(val), sup.FormatType(fused))
				vec := defuse.Eval(sbuf.Dematerialize(sctx, up))
				back := vector.ValueAt(scode.NewBuilder(), vec, 0)
				require.Equal(t, sup.FormatValue(val), sup.FormatValue(back), "fused type %s", sup.FormatType(fused))
			}
		})
	}
}

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
