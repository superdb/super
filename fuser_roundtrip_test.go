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
			defuse := expr.NewDefuse(sctx)
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
