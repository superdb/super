package expr

import (
	"net/netip"
	"regexp"
	"slices"
	"unsafe"

	"github.com/superdb/super"
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
	eq := NewCompare(super.NewContext() /* XXX */, "==", nil, nil)
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
	return &search{sctx, e, nil, re.Match}
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
	var index []uint32
	if view, ok := vec.(*vector.View); ok {
		vec = view.Any
		index = view.Index
	}
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
		return s.eval(vec.Vals)
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
