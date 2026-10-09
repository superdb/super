package sup

import (
	"unicode"
)

func IsIdentifier(s string) bool {
	if s == "" {
		return false
	}
	first := true
	for _, c := range s {
		if !idChar(c) && (first || !unicode.IsDigit(c)) {
			return false
		}
		first = false
	}
	return true
}

func idChar(c rune) bool {
	return unicode.IsLetter(c) || c == '_' || c == '$' || c == '.'
}

// IsTypeName returns true iff s is a valid, unquoted SUP type name.
func IsTypeName(s string) bool {
	for k, c := range s {
		if !typeChar(c) {
			return false
		}
		if k == 0 && unicode.IsDigit(c) {
			return false
		}
	}
	return true
}

func typeChar(c rune) bool {
	return idChar(c) || unicode.IsDigit(c) || c == '.'
}
