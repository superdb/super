package search

import (
	"strings"
	"unicode/utf8"

	"github.com/superdb/super/pkg/stringsearch"
)

type Expr interface {
	Eval([]string) bool
}

type and struct {
	left  Expr
	right Expr
}

type or struct {
	left  Expr
	right Expr
}

func NewAndExpr(left, right Expr) Expr {
	return &and{left, right}
}

func (a *and) Eval(bufs []string) bool {
	return a.left.Eval(bufs) && a.right.Eval(bufs)

}

func NewOrExpr(left, right Expr) Expr {
	return &or{left, right}
}

func (o *or) Eval(bufs []string) bool {
	return o.left.Eval(bufs) || o.right.Eval(bufs)

}

type leaf struct {
	next func(string) int
}

func (l *leaf) Eval(bufs []string) bool {
	for _, buf := range bufs {
		if l.next(buf) > -1 {
			return true
		}
	}
	return false
}

func NewExprForString(pattern string) Expr {
	if len(pattern) < 2 {
		return nil // very short patterns are unprofitable
	}
	return &leaf{stringsearch.NewFinder(pattern).Next}
}

func NewExprForStringCase(pattern string) Expr {
	if len(pattern) < 2 {
		return nil
	}
	for _, r := range pattern {
		if r >= utf8.RuneSelf {
			return nil // CaseFinder is case-sensitive for multibyte letters
		}
	}
	return &leaf{stringsearch.NewCaseFinder(pattern).Next}
}

// NewExprForAnyString returns an Expr that is true for text containing any of
// patterns.  Unlike NewExprForString, it is meant for short patterns.
func NewExprForAnyString(patterns []string) Expr {
	return &leaf{func(text string) int {
		for _, p := range patterns {
			if i := strings.Index(text, p); i >= 0 {
				return i
			}
		}
		return -1
	}}
}
