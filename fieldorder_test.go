package super_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/sup"
)

func TestFuserFieldOrder(t *testing.T) {
	cases := []struct {
		types    []string
		expected string
	}{
		// Field orders that agree are preserved.
		{[]string{"{b:int64,a:int64}", "{b:int64,a:int64,c:int64|missing}"}, "{b:int64,a:int64,c:int64|missing}"},
		{[]string{"{x:int64,y:int64}", "{y:int64,z:int64}"}, "{x:int64|missing,y:int64,z:int64|missing}"},
		// Nested records and records in containers and unions.
		{[]string{"{r:{z:int64,y:int64}}", "{r:{z:string}}"}, "{r:{z:int64|string,y:int64|missing}}"},
		{[]string{"[{b:int64,a:int64}]", "[{b:int64}]"}, "[{b:int64,a:int64|missing}]"},
		{[]string{"{b:int64,a:int64}|string", "{b:int64}"}, "string|{b:int64,a:int64|missing}"},
		// The majority order wins.
		{[]string{"{a:int64,b:int64}", "{b:int64,a:int64,c:int64}", "{b:int64,a:int64,d:int64}"}, "{b:int64,a:int64,c:int64|missing,d:int64|missing}"},
		// Orders that don't conflict are preserved even when Copeland
		// score alone would put a field that beats more fields first.
		{[]string{"{a:int64,r:int64,key:int64}", "{r:int64,s:int64,r2:int64,key:int64}"}, "{a:int64|missing,r:int64,s:int64|missing,r2:int64|missing,key:int64}"},
		// A field isn't penalized for losing to a field already placed.
		{[]string{"{x:int64,y:int64}", "{z:int64}"}, "{x:int64|missing,y:int64|missing,z:int64|missing}"},
		// Ties and cycles are broken by Copeland score, then net margin,
		// and then name.
		{[]string{"{a:int64,b:int64}", "{b:int64,a:int64}"}, "{a:int64,b:int64}"},
		{[]string{"{a:int64,b:int64}", "{b:int64,c:int64}", "{c:int64,a:int64}"}, "{a:int64|missing,b:int64|missing,c:int64|missing}"},
		{[]string{"{c:int64}", "{a:int64}", "{b:int64}"}, "{a:int64|missing,b:int64|missing,c:int64|missing}"},
	}
	for _, c := range cases {
		t.Run(c.expected, func(t *testing.T) {
			// Every permutation of the input types yields the same type.
			for _, perm := range permutations(c.types) {
				sctx := super.NewContext()
				fuser := super.NewFuser(sctx)
				for _, s := range perm {
					typ, err := sup.ParseType(sctx, s)
					require.NoError(t, err)
					fuser.Fuse(typ)
				}
				require.Equal(t, c.expected, sup.FormatType(fuser.SuperType()), "input order %v", perm)
			}
		})
	}
}

func permutations(s []string) [][]string {
	if len(s) <= 1 {
		return [][]string{s}
	}
	var out [][]string
	for i := range s {
		rest := append(append([]string{}, s[:i]...), s[i+1:]...)
		for _, p := range permutations(rest) {
			out = append(out, append([]string{s[i]}, p...))
		}
	}
	return out
}
