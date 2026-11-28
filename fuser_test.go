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
// no super types and that each value upcasts to the fused type and defuses
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
			require.NotContains(t, sup.FormatType(fuser.SuperType()), "super", "supertype has super")
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
		{[]string{"any", "int64"}, "any", "super(all)"},
		{[]string{"any", "{a:int64}", "[string]"}, "any", "super(all)"},
		{[]string{"any", "null"}, "any", "super(all)"},
		// any absorbs everything but missing.
		{[]string{"any", "missing"}, "missing|any", "super(all)"},
		{[]string{"any", "int64|missing"}, "missing|any", "super(all)"},
		{[]string{"{a:any}", "{a:int64}"}, "{a:any}", "{a:super(all)}"},
		{[]string{"{a:any}", "{b:int64}"}, "{a:missing|any,b:int64|missing}", "{a:super(all),b:super(int64|missing)}"},
		{[]string{"[any]", "[int64]"}, "[any]", "super([super(all)])"},
		{[]string{"{a:any}|string", "string"}, "string|{a:any}", "super(string|{a:super(all)})"},
		// A fused type with super(all) normalizes to any in the supertype.
		{[]string{"super(all)", "int64"}, "any", "super(all)"},
		{[]string{"{a:super(all)}", "{a:string}"}, "{a:any}", "{a:super(all)}"},
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

func TestAnyToSuper(t *testing.T) {
	cases := []struct{ in, out string }{
		{"any", "super(all)"},
		{"int64", "int64"},
		{"{a:any,b:[any],c:int64}", "{a:super(all),b:[super(all)],c:int64}"},
		{"any|missing", "super(all)"},
		{"{a:any}|string", "string|{a:super(all)}"},
		{"any|string", "super(all)"},
		{"map{string:any}", "map{string:super(all)}"},
	}
	for _, c := range cases {
		sctx := super.NewContext()
		typ, err := sup.ParseType(sctx, c.in)
		require.NoError(t, err)
		require.Equal(t, c.out, sup.FormatType(sctx.AnyToSuper(typ)), c.in)
	}
}
