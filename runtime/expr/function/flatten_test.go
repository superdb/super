package function

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/sup"
)

func formatPath(path []super.Value) string {
	var elems []string
	for _, elem := range path {
		elems = append(elems, sup.FormatValue(elem))
	}
	return "[" + strings.Join(elems, ",") + "]"
}

func TestParsePath(t *testing.T) {
	sctx := super.NewContext()
	cases := []struct {
		path     string
		expected string
	}{
		{`"x.1.y"`, `["x",1,"y"]`},
		{`"x"`, `["x"]`},
		{`"0"`, `[0]`},
		{`"a.-1.01"`, `["a","-1",1]`},
		{`["x",1,"y"]`, `["x",1,"y"]`},
		{`[10.0.0.1,"a"]`, `[10.0.0.1,"a"]`},
		{`[]::[string]`, `[]`},
	}
	for _, c := range cases {
		val, err := sup.ParseValue(sctx, c.path)
		require.NoError(t, err, c.path)
		path, err := ParsePath(val)
		require.NoError(t, err, c.path)
		require.Equal(t, c.expected, formatPath(path), c.path)
	}
	val, err := sup.ParseValue(sctx, `1`)
	require.NoError(t, err)
	_, err = ParsePath(val)
	require.EqualError(t, err, "path must be an array or string: 1")
}

func TestParsePaths(t *testing.T) {
	sctx := super.NewContext()
	cases := []struct {
		paths    string
		expected string
	}{
		{`"x.1.y,a.b"`, `["x",1,"y"] ["a","b"]`},
		{`"x"`, `["x"]`},
		{`[["x",1,"y"],["a","b"]]`, `["x",1,"y"] ["a","b"]`},
	}
	for _, c := range cases {
		val, err := sup.ParseValue(sctx, c.paths)
		require.NoError(t, err, c.paths)
		paths, err := ParsePaths(val)
		require.NoError(t, err, c.paths)
		var actual []string
		for _, path := range paths {
			actual = append(actual, formatPath(path))
		}
		require.Equal(t, c.expected, strings.Join(actual, " "), c.paths)
	}
	// A path abbreviation can't be used inside an array of paths.
	val, err := sup.ParseValue(sctx, `["x.1.y",["a"]]`)
	require.NoError(t, err)
	_, err = ParsePaths(val)
	require.EqualError(t, err, `path in a path collection must be an array: "x.1.y"`)
}
