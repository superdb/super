package expr

import (
	"bytes"
	"net/netip"
	"regexp"
	"slices"
	"unicode"
	"unicode/utf8"
	"unsafe"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/stringsearch"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/vector"
)

type search struct {
	sctx       *super.Context
	e          Evaluator
	vectorPred func(vector.Any) vector.Any
	stringPred func([]byte) bool
}

func NewSearch(sctx *super.Context, s string, val super.Value, e Evaluator) Evaluator {
	stringPred := func(b []byte) bool {
		return expr.StringContainsFold(string(b), s)
	}
	var net netip.Prefix
	if val.Type().ID() == super.IDNet {
		net = super.DecodeNet(val.Bytes())
	}
	eq := NewLenientCompare(super.NewContext() /* XXX */, "==")
	vectorPred := func(vec vector.Any) vector.Any {
		if net.IsValid() && vec.Kind() == vector.KindIP {
			out := vector.NewFalse(vec.Len())
			for i := range vec.Len() {
				if net.Contains(vector.IPValue(vec, i)) {
					out.Set(i)
				}
			}
			return out
		}
		if val.IsNull() {
			return vector.NewNull(vec.Len())
		}
		return eq.eval(vec, vector.NewConstFromValue(sctx, val, vec.Len()))
	}
	return &search{sctx, e, vectorPred, stringPred}
}

func NewSearchRegexp(sctx *super.Context, re *regexp.Regexp, e Evaluator) Evaluator {
	return &search{sctx, e, nil, regexpPred(re)}
}

// regexpPred returns a function equivalent to re.Match that avoids the
// regexp engine when re is a literal or a case-insensitive literal.
func regexpPred(re *regexp.Regexp) func([]byte) bool {
	literal, fold, ok := expr.ParseLiteral(re.String())
	if !ok {
		return re.Match
	}
	if !fold {
		b := []byte(literal)
		return func(s []byte) bool {
			return bytes.Contains(s, b)
		}
	}
	contains := foldContains(literal)
	var odd [][]byte
	for _, o := range expr.OddFoldVariants(literal) {
		odd = append(odd, []byte(o))
	}
	return func(s []byte) bool {
		if contains(s) {
			return true
		}
		for _, o := range odd {
			if bytes.Contains(s, o) {
				return re.Match(s)
			}
		}
		return false
	}
}

// foldContains returns a function that reports whether its argument contains
// literal, ignoring case as StringContainsFold does.
func foldContains(literal string) func([]byte) bool {
	for _, r := range literal {
		if r >= utf8.RuneSelf && (unicode.SimpleFold(r) != r || unicode.ToLower(r) != r) {
			return func(s []byte) bool {
				return expr.StringContainsFold(string(s), literal)
			}
		}
	}
	// Ignoring ASCII case is enough when no other letter in literal has a
	// case variant, as long as NewCaseFinder's lowering leaves it as is.
	f := stringsearch.NewCaseFinder(literal)
	return func(s []byte) bool {
		return f.Next(unsafe.String(unsafe.SliceData(s), len(s))) >= 0
	}
}

func NewSearchString(sctx *super.Context, s string, e Evaluator) Evaluator {
	pred := func(b []byte) bool {
		return expr.StringContainsFold(string(b), s)
	}
	return &search{sctx, e, nil, pred}
}

// Eval searches the result of s.e.  Errors, nulls, nones, and missings that
// s.e itself produces are caught here, type by type, rather than searched.
// Values nested inside the result are data to search (see eval).
func (s *search) Eval(this vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipFusions|vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		if out, ok := Catch(s.sctx, CatchAll & ^CatchOption, vecs[0], "search"); ok {
			return out
		}
		return s.applyEval(vecs[0])
	}, s.e.Eval(this))
}

func (s *search) applyEval(vec vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipFusions|vector.ApplyRipUnions|vector.ApplyRipOptions, s.eval, vec)
}

func (s *search) eval(vecs ...vector.Any) vector.Any {
	vec := vector.Under(vecs[0])
	// Search doesn't defuse so we need to check for missings (and of course
	// none never matches a search).
	if k := vec.Kind(); k == vector.KindMissing || k == vector.KindNone {
		return vector.NewFalse(vec.Len())
	}
	typ := vec.Type()
	if typ.Kind() == super.PrimitiveKind {
		return s.match(vec)
	}
	n := vec.Len()
	vec, index := vector.UnwrapViewOrConst(vec)
	switch vec := vec.(type) {
	case *vector.Record:
		out := vector.NewFalse(n)
		for _, f := range vec.Fields {
			if index != nil {
				f = vector.Pick(f, index)
			}
			vec2 := s.applyEval(f)
			if vec2.Kind() != vector.KindNull {
				out = vector.Or(out, FlattenBool(vec2))
			}
		}
		return out
	case *vector.Array:
		return s.evalForList(vec.Values, vec.Offsets, index, n)
	case *vector.Set:
		return s.evalForList(vec.Values, vec.Offsets, index, n)
	case *vector.Map:
		return vector.Or(s.evalForList(vec.Keys, vec.Offsets, index, n),
			s.evalForList(vec.Values, vec.Offsets, index, n))
	case *vector.Error:
		return s.applyEval(vec.Vals)
	case *vector.Option:
		if none, ok := vec.Any.(*vector.None); ok {
			return vector.NewFalse(none.Len())
		}
		return s.applyEval(vec.Any)
	}
	panic(vector.Format(vec))
}

func (s *search) evalForList(vec vector.Any, offsets, index []uint32, length uint32) *vector.Bool {
	out := vector.NewFalse(length)
	var index2 []uint32
	for j := range length {
		idx := j
		if index != nil {
			idx = index[j]
		}
		start, end := offsets[idx], offsets[idx+1]
		if start == end {
			continue
		}
		n := end - start
		index2 = slices.Grow(index2[:0], int(n))[:n]
		for k := range n {
			index2[k] = k + start
		}
		vec := s.applyEval(vector.Pick(vec, index2))
		if FlattenBool(vec).Bits.TrueCount() > 0 {
			out.Set(j)
		}
	}
	return out
}

func (s *search) match(vec vector.Any) vector.Any {
	if vec.Type().ID() == super.IDString {
		out := vector.NewFalse(vec.Len())
		for i := range vec.Len() {
			str := vector.StringValue(vec, i)
			// Prevent compiler from copying str, which it thinks
			// escapes to the heap because stringPred is a pointer.
			bytes := unsafe.Slice(unsafe.StringData(str), len(str))
			if s.stringPred(bytes) {
				out.Set(i)
			}
		}
		return out
	}
	if s.vectorPred != nil {
		return s.vectorPred(vec)
	}
	return vector.NewConstBool(false, vec.Len())
}

type regexpMatch struct {
	sctx *super.Context
	re   *regexp.Regexp
	e    Evaluator
}

func NewRegexpMatch(sctx *super.Context, re *regexp.Regexp, e Evaluator) Evaluator {
	return &regexpMatch{sctx, re, e}
}

func (r *regexpMatch) Eval(this vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipUnions|vector.ApplyRipFusions, r.eval, r.e.Eval(this))
}

func (r *regexpMatch) eval(vecs ...vector.Any) vector.Any {
	// Catch per type, after Apply has split up any dynamic vector, so a null
	// or error in mixed-type input is caught like one in uniform input.
	if out, ok := Catch(r.sctx, CatchAll & ^CatchOption, vecs[0], "regexp"); ok {
		return out
	}
	vec := vector.Under(vecs[0])
	if vec.Kind() != vector.KindString {
		return vector.NewConstBool(false, vec.Len())
	}
	out := vector.NewFalse(vec.Len())
	for i := range vec.Len() {
		s := vector.StringValue(vec, i)
		if r.re.MatchString(s) {
			out.Set(i)
		}
	}
	return out
}
