package demand

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdb/super/pkg/field"
)

func path(d Demand, keys ...string) Demand {
	for _, k := range slices.Backward(keys) {
		d = Key(k, d)
	}
	return d
}

func paths(fields []field.Path) string {
	var ss []string
	for _, f := range fields {
		ss = append(ss, f.String())
	}
	slices.Sort(ss)
	return strings.Join(ss, ",")
}

func TestTypes(t *testing.T) {
	cases := []struct {
		demand Demand
		fields string
		types  string
	}{
		// typeof(a.b), typeof(a)
		{Union(path(TypesOnly(), "a", "b"), path(TypesOnly(), "a")), "", "a"},
		// typeof(a), a.b
		{Union(path(TypesOnly(), "a"), path(All(), "a", "b")), "a.b", "a"},
		// typeof(a), a.b.c (b's type is implied by a's type)
		{Union(path(TypesOnly(), "a"), path(All(), "a", "b", "c")), "a.b.c", "a,a.b"},
		// typeof(a.b), a
		{Union(path(TypesOnly(), "a", "b"), path(All(), "a")), "a", ""},
		// typeof(a), typeof(b), c
		{Union(path(TypesOnly(), "a"), path(TypesOnly(), "b"), path(All(), "c")), "c", "a,b"},
		// typeof(this), a
		{Union(TypesOnly(), path(All(), "a")), "a", "this"},
	}
	for _, c := range cases {
		require.True(t, IsValid(c.demand))
		require.Equal(t, c.fields, paths(Fields(c.demand)))
		require.Equal(t, c.types, paths(TypeFields(c.demand)))
	}
	d := WithoutTypes(Union(path(TypesOnly(), "a"), path(All(), "a", "b"), path(All(), "c")))
	require.Equal(t, "a,c", paths(Fields(d)))
	require.Equal(t, "", paths(TypeFields(d)))
	require.True(t, IsAll(Union(TypesOnly(), All())))
	require.Equal(t, TypesOnly(), GetKey(TypesOnly(), "x"))
}
