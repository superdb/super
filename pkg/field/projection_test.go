package field

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProjection(t *testing.T) {
	cases := [][]Path{
		{
			Path{"a", "b"},
		},
		{
			Path{"a", "b"},
			Path{"c", "d"},
		},
		{
			Path{"a", "b"},
			Path{"a", "c"},
		},
		{
			Path{"a", "b", "c"},
			Path{"a", "b", "d"},
		},
	}
	for _, c := range cases {
		proj := NewProjection(c)
		assert.Equal(t, c, proj.Paths(), "projection: %#v", proj)
	}
}

func TestFlattenAsserts(t *testing.T) {
	// a.(T1)[x, b.(T2)] and c.(T3), which needs all of the data.
	inner := NewProjectionMaybe(true, []Path{{"x"}}, []Path{{}}, nil)
	inner = append(inner, ProjectionNode{Name: "b", Proj: Projection{{Assert: true, TypeID: 2, Proj: Projection{{Types: true}}}}})
	p := Projection{
		{Name: "a", Proj: Projection{{Assert: true, TypeID: 1, Proj: inner}}},
		{Name: "c", Proj: Projection{{Assert: true, TypeID: 3}}},
	}
	flat := FlattenAsserts(p)
	assert.Equal(t, []AssertPath{
		{Parent: -1, Path: Path{"a"}, TypeID: 1, HasProjection: true, Projection: []Path{{"x"}}, TypeProjection: []Path{nil}},
		{Parent: 0, Path: Path{"b"}, TypeID: 2, HasProjection: true, TypeProjection: []Path{nil}},
		{Parent: -1, Path: Path{"c"}, TypeID: 3},
	}, flat)
	assert.Equal(t, flat, FlattenAsserts(UnflattenAsserts(flat)))
	assert.True(t, p.HasTypesAnywhere())
	assert.Nil(t, Projection{{Assert: true, TypeID: 1}}.NoTypes())
	assert.Equal(t, []Path{{"a"}, {"c"}}, p.Paths())
}
