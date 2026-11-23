//go:build js && wasm

package jsonskip

import (
	"github.com/bytedance/sonic/ast"

	"github.com/superdb/super/pkg/byteconv"
)

// Skip skips a single JSON value in b, returning the offset of its first
// non-whitespace byte and the offset just past its end.  If b does not
// begin with a complete JSON value, start is negative.
//
// sonic's decoder.Skip is unavailable on js/wasm, but a Searcher with an
// empty path uses sonic's pure-Go skipper and returns the raw value.
func Skip(b []byte) (int, int) {
	s := byteconv.UnsafeString(b)
	node, err := ast.NewSearcher(s).GetByPath()
	if err != nil {
		return -1, 0
	}
	raw, err := node.Raw()
	if err != nil {
		return -1, 0
	}
	start := firstNonSpace(b)
	return start, start + len(raw)
}

func firstNonSpace(b []byte) int {
	for i, c := range b {
		switch c {
		case ' ', '\t', '\n', '\r':
		default:
			return i
		}
	}
	return len(b)
}
