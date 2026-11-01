package vbuild_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
)

func TestMergeTypeOnly(t *testing.T) {
	sctx := super.NewContext()
	typ := sctx.MustLookupTypeRecord([]super.Field{
		super.NewField("a", super.TypeInt64),
		super.NewField("b", super.TypeString),
	})
	rec := func(vals ...int64) vector.Any {
		n := uint32(len(vals))
		return vector.NewRecord(typ, []vector.Any{
			vector.NewInt(super.TypeInt64, vals),
			vector.NewTypeOnly(super.TypeString, n),
		}, n)
	}
	vec := vbuild.Merge([]uint32{0, 1, 0}, []vector.Any{rec(1, 3), rec(2)})
	out := vec.(*vector.Record)
	require.Equal(t, uint32(3), out.Len())
	var b scode.Builder
	for i, want := range []string{"1", "2", "3"} {
		require.Equal(t, want, sup.FormatValue(vector.ValueAt(&b, out.Fields[0], uint32(i))))
	}
	require.IsType(t, (*vector.TypeOnly)(nil), out.Fields[1])
	require.Equal(t, uint32(3), out.Fields[1].Len())
}
