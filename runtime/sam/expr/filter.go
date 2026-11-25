package expr

import (
	"regexp/syntax"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdb/super"
)

// StringContainsFold is like strings.Contains but with case-insensitive
// comparison.
func StringContainsFold(a, b string) bool {
	alen := len(a)
	blen := len(b)

	if blen > alen {
		return false
	}

	end := alen - blen + 1
	i := 0
	for i < end {
		if strings.EqualFold(a[i:i+blen], b) {
			return true
		}
		i++
	}
	return false
}

// ParseLiteral reports whether pattern is a regular expression that matches
// just a literal string, such as "foo", or the literal ignoring case, such as
// "(?i)foo".  If so, it returns the literal and whether case is ignored.
func ParseLiteral(pattern string) (string, bool, bool) {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil || re.Op != syntax.OpLiteral || slices.Contains(re.Rune, utf8.RuneError) {
		return "", false, false
	}
	return string(re.Rune), re.Flags&syntax.FoldCase != 0, true
}

// OddFoldVariants returns the case variants of the letters in literal whose
// UTF-8 encodings have a different length (e.g., the Kelvin sign for "k"),
// which StringContainsFold doesn't match.
func OddFoldVariants(literal string) []string {
	var odd []string
	for _, r := range literal {
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if utf8.RuneLen(f) != utf8.RuneLen(r) && !slices.Contains(odd, string(f)) {
				odd = append(odd, string(f))
			}
		}
	}
	return odd
}

func IsTrue(val super.Value) bool {
	return val.Deunion().Ptr().AsBool()
}
