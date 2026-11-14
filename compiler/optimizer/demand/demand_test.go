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

func TestDelete(t *testing.T) {
	cases := []struct {
		demand Demand
		fields string
		types  string
	}{
		// typeof(this), x minus x
		{Delete(Union(TypesOnly(), path(All(), "x")), path(All(), "x")), "", "this"},
		// typeof(this), a.b, a.c minus a.b
		{Delete(Union(TypesOnly(), path(All(), "a", "b"), path(All(), "a", "c")), path(All(), "a", "b")), "a.c", "a,this"},
		// typeof(this), a.b minus a.b
		{Delete(Union(TypesOnly(), path(All(), "a", "b")), path(All(), "a", "b")), "", "this"},
		// a.b minus a.b
		{Delete(path(All(), "a", "b"), path(All(), "a", "b")), "", ""},
		// a.b, c minus a.b
		{Delete(Union(path(All(), "a", "b"), path(All(), "c")), path(All(), "a", "b")), "c", ""},
	}
	for _, c := range cases {
		require.True(t, IsValid(c.demand))
		require.Equal(t, c.fields, paths(Fields(c.demand)))
		require.Equal(t, c.types, paths(TypeFields(c.demand)))
	}
}

func TestAsserts(t *testing.T) {
	assertA := Assert(1, path(All(), "x"))
	assertB := Assert(2, path(All(), "y"))
	// a.(A).x, a.(B).y
	d := Union(path(assertA, "a"), path(assertB, "a"))
	require.True(t, IsValid(d))
	require.Equal(t, "", paths(Fields(d)))
	require.Equal(t, "", paths(TypeFields(d)))
	p := AssertProjection(d)
	require.Len(t, p, 1)
	require.Equal(t, "a", p[0].Name)
	require.Len(t, p[0].Proj, 2)
	// The projection of each assertion keeps the asserted type.
	require.True(t, p[0].Proj[0].Proj.HasTypes())
	// typeof(this), a.(A).x keeps the assertion.
	d = Union(TypesOnly(), path(assertA, "a"))
	require.Equal(t, "this", paths(TypeFields(d)))
	require.Len(t, AssertProjection(d), 1)
	// a.(A).x, typeof(a) keeps the assertion too.
	d = Union(path(TypesOnly(), "a"), path(assertA, "a"))
	require.Equal(t, "", paths(TypeFields(d)))
	require.Len(t, AssertProjection(d), 1)
	// a.(A).x, a.z needs the type of a and the data of a.x and a.z.
	d = Union(path(assertA, "a"), path(All(), "a", "z"))
	require.True(t, IsValid(d))
	require.Equal(t, "a.x,a.z", paths(Fields(d)))
	require.Equal(t, "a", paths(TypeFields(d)))
	require.Nil(t, AssertProjection(d))
	// a.(A).x, a needs all of a.
	d = Union(path(assertA, "a"), path(All(), "a"))
	require.Equal(t, "a", paths(Fields(d)))
	require.Nil(t, AssertProjection(d))
	// A materializing operator needs all of a.
	require.Equal(t, "a", paths(Fields(WithoutTypes(path(assertA, "a")))))
	// Without assertions, a.(A).x needs the type of a and the data of a.x.
	d = WithoutAsserts(path(assertA, "a"))
	require.Equal(t, "a.x", paths(Fields(d)))
	require.Equal(t, "a", paths(TypeFields(d)))
}
