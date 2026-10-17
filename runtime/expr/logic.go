package expr

import (
	"slices"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/bitvec"
)

type Not struct {
	sctx *super.Context
	expr Evaluator
}

var _ Evaluator = (*Not)(nil)

func NewLogicalNot(sctx *super.Context, e Evaluator) *Not {
	return &Not{sctx, e}
}

func (n *Not) Eval(val vector.Any) vector.Any {
	return evalBool(n.sctx, n.eval, n.expr.Eval(val))
}

func (n *Not) eval(vecs ...vector.Any) vector.Any {
	if vec, ok := Catch(n.sctx, true, vecs, "'not' operator"); ok {
		return vec
	}
	switch vec := vecs[0].(type) {
	case *vector.Bool:
		return vector.Not(vec)
	case *vector.Const:
		v := !vector.BoolValue(vec, 0)
		return vector.NewConstBool(v, vec.Len())
	default:
		panic(vec)
	}
}

type And struct {
	sctx *super.Context
	lhs  Evaluator
	rhs  Evaluator
}

func NewLogicalAnd(sctx *super.Context, lhs, rhs Evaluator) *And {
	return &And{sctx, lhs, rhs}
}

func (a *And) Eval(val vector.Any) vector.Any {
	return evalBool(a.sctx, a.eval, a.lhs.Eval(val), a.rhs.Eval(val))
}

func (a *And) eval(vecs ...vector.Any) vector.Any {
	lhs, rhs := vecs[0], vecs[1]
	lhsKind, rhsKind := lhs.Kind(), rhs.Kind()
	switch {
	case lhsKind == vector.KindNull:
		if rhsKind == vector.KindNull {
			return lhs
		}
		if rhsKind == vector.KindError {
			return rhs
		}
		return a.evalWithNullOrError(rhs, lhs)
	case rhsKind == vector.KindNull:
		if lhsKind == vector.KindError {
			return lhs
		}
		return a.evalWithNullOrError(lhs, rhs)
	case lhsKind == vector.KindError:
		if rhsKind == vector.KindError {
			return lhs
		}
		return a.evalWithNullOrError(rhs, lhs)
	case rhsKind == vector.KindError:
		return a.evalWithNullOrError(lhs, rhs)
	}
	blhs, brhs := FlattenBool(lhs), FlattenBool(rhs)
	return vector.NewBool(bitvec.And(blhs.Bits, brhs.Bits))
}

func (*And) evalWithNullOrError(boolVec, nullOrErrorVec vector.Any) vector.Any {
	// true and nullOrError = nullOrError
	// false and any = false
	var index []uint32
	for i := range boolVec.Len() {
		if vector.BoolValue(boolVec, i) {
			index = append(index, i)
		}
	}
	return combine(boolVec, nullOrErrorVec, index)
}

func combine(baseVec, overlayVec vector.Any, index []uint32) vector.Any {
	if len(index) == 0 {
		return baseVec
	}
	if len(index) == int(overlayVec.Len()) {
		return overlayVec
	}
	baseVec = vector.ReversePick(baseVec, index)
	overlayVec = vector.Pick(overlayVec, index)
	return vector.Combine(baseVec, index, overlayVec)
}

func EvalOr(sctx *super.Context, lhs, rhs vector.Any) vector.Any {
	return evalBool(sctx, (*Or)(nil).eval, lhs, rhs)
}

type Or struct {
	sctx *super.Context
	lhs  Evaluator
	rhs  Evaluator
}

func NewLogicalOr(sctx *super.Context, lhs, rhs Evaluator) *Or {
	return &Or{sctx, lhs, rhs}
}

func (o *Or) Eval(val vector.Any) vector.Any {
	return evalBool(o.sctx, o.eval, o.lhs.Eval(val), o.rhs.Eval(val))
}

func (o *Or) eval(vecs ...vector.Any) vector.Any {
	lhs, rhs := vecs[0], vecs[1]
	switch lhsKind, rhsKind := lhs.Kind(), rhs.Kind(); {
	case lhsKind == vector.KindNull:
		if rhsKind == vector.KindNull || rhsKind == vector.KindError {
			return lhs
		}
		return o.evalWithNullOrError(rhs, lhs)
	case rhsKind == vector.KindNull:
		if lhsKind == vector.KindError {
			return rhs
		}
		return o.evalWithNullOrError(lhs, rhs)
	case lhsKind == vector.KindError:
		if rhsKind == vector.KindError {
			return lhs
		}
		return o.evalWithNullOrError(rhs, lhs)
	case rhsKind == vector.KindError:
		return o.evalWithNullOrError(lhs, rhs)
	}
	return vector.Or(FlattenBool(lhs), FlattenBool(rhs))
}

func (*Or) evalWithNullOrError(boolVec, nullOrErrorVec vector.Any) vector.Any {
	// false or nullOrError = nullOrError
	// true or any = true
	var index []uint32
	for i := range boolVec.Len() {
		if !vector.BoolValue(boolVec, i) {
			index = append(index, i)
		}
	}
	return combine(boolVec, nullOrErrorVec, index)
}

// evalBool evaluates e using val to computs a boolean result.  For elements
// of the result that are not boolean, an error is calculated for each non-bool
// slot and they are returned as an error.  If all of the value slots are errors,
// then the return value is nil.
func evalBool(sctx *super.Context, fn func(...vector.Any) vector.Any, vecs ...vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipUnions|vector.ApplyRipFusions, func(vecs ...vector.Any) vector.Any {
		for i, vec := range vecs {
			vec := vector.Under(vec)
			if k := vec.Kind(); k == vector.KindBool || k == vector.KindNull || k == vector.KindError {
				vecs[i] = vec
			} else {
				vecs[i] = vector.NewWrappedError(sctx, "not type bool", vec)
			}
		}
		return fn(vecs...)
	}, vecs...)
}

func FlattenBool(vec vector.Any) *vector.Bool {
	switch vec := vec.(type) {
	case *vector.Const:
		if vector.BoolValue(vec, 0) {
			return vector.NewTrue(vec.Len())
		}
		return vector.NewFalse(vec.Len())
	case *vector.Dynamic:
		out := vector.NewFalse(vec.Len())
		for i := range vec.Len() {
			if vector.BoolValue(vec, i) {
				out.Set(i)
			}
		}
		return out
	case *vector.Bool:
		return vec
	default:
		panic(vec)
	}
}

type In struct {
	sctx   *super.Context
	lhs    Evaluator
	rhs    Evaluator
	defuse *Defuse
	pw     *PredicateWalk
}

func NewIn(sctx *super.Context, lhs, rhs Evaluator) *In {
	return &In{sctx, lhs, rhs, NewDefuse(sctx), NewPredicateWalk(sctx, NewCompare(sctx, "==", nil, nil).eval)}
}

func (i *In) Eval(this vector.Any) vector.Any {
	lhs := i.defuse.Eval(i.lhs.Eval(this))
	rhs := i.defuse.Eval(i.rhs.Eval(this))
	return vector.Apply(vector.ApplyRipUnions, i.eval, lhs, rhs)
}

func (i *In) eval(vecs ...vector.Any) vector.Any {
	if vec, ok := Catch(i.sctx, true, vecs, "'in' operator"); ok {
		return vec
	}
	return i.pw.Eval(vecs[0], vecs[1])
}

type PredicateWalk struct {
	sctx *super.Context
	pred func(...vector.Any) vector.Any
}

func NewPredicateWalk(sctx *super.Context, pred func(...vector.Any) vector.Any) *PredicateWalk {
	return &PredicateWalk{sctx, pred}
}

func (p *PredicateWalk) Eval(vecs ...vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipUnions, p.eval, vecs...)
}

func (p *PredicateWalk) eval(vecs ...vector.Any) vector.Any {
	lhs, rhs := vecs[0], vecs[1]
	out := p.pred(lhs, rhs)
	rhs = vector.Under(rhs)
	var index []uint32
	if view, ok := rhs.(*vector.View); ok {
		rhs = view.Any
		index = view.Index
	}
	switch rhs := rhs.(type) {
	case *vector.Record:
		for _, vec := range rhs.Fields {
			if index != nil {
				vec = vector.Pick(vec, index)
			}
			out = EvalOr(p.sctx, out, p.Eval(lhs, vec))
		}
		return out
	case *vector.Array:
		return EvalOr(p.sctx, out, p.evalForList(lhs, rhs.Values, rhs.Offsets, index))
	case *vector.Set:
		return EvalOr(p.sctx, out, p.evalForList(lhs, rhs.Values, rhs.Offsets, index))
	case *vector.Map:
		out = EvalOr(p.sctx, out, p.evalForList(lhs, rhs.Keys, rhs.Offsets, index))
		return EvalOr(p.sctx, out, p.evalForList(lhs, rhs.Values, rhs.Offsets, index))
	case *vector.Union:
		if index != nil {
			panic("vector.Union unexpected in vector.View")
		}
		return EvalOr(p.sctx, out, vector.Apply(vector.ApplyRipUnions, p.Eval, lhs, rhs))
	case *vector.Error:
		if index != nil {
			panic("vector.Error unexpected in vector.View")
		}
		return EvalOr(p.sctx, out, p.Eval(lhs, rhs.Vals))
	default:
		return out
	}
}

func (p *PredicateWalk) evalForList(lhs, rhs vector.Any, offsets, index []uint32) vector.Any {
	n := lhs.Len()
	var nulls, trues roaring.Bitmap
	var lhsIndex, rhsIndex []uint32
	for j := range n {
		idx := j
		if index != nil {
			idx = index[j]
		}
		start, end := offsets[idx], offsets[idx+1]
		if start == end {
			continue
		}
		n := end - start
		lhsIndex = slices.Grow(lhsIndex[:0], int(n))[:n]
		rhsIndex = slices.Grow(rhsIndex[:0], int(n))[:n]
		for k := range n {
			lhsIndex[k] = j
			rhsIndex[k] = k + start
		}
		lhsView := vector.Pick(lhs, lhsIndex)
		rhsView := vector.Pick(rhs, rhsIndex)
		vec := p.Eval(lhsView, rhsView)
		if hasTrue(vec) {
			trues.Add(j)
		} else if hasNull(vec) {
			nulls.Add(j)
		}
	}
	truesVec := vector.NewFalse(n)
	trues.WriteDenseTo(truesVec.GetBits())
	nullsVec := vector.NewNull(n)
	return combine(truesVec, nullsVec, nulls.ToArray())
}

func hasNull(vec vector.Any) bool {
	var hasNull bool
	vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		hasNull = hasNull || vecs[0].Kind() == vector.KindNull
		return vecs[0]
	}, vec)
	return hasNull
}

func hasTrue(vec vector.Any) bool {
	var hasTrue bool
	vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
		vec := vecs[0]
		if !hasTrue && vec.Kind() == vector.KindBool {
			for i := range vec.Len() {
				if vector.BoolValue(vec, i) {
					hasTrue = true
					break
				}
			}
		}
		return vec
	}, vec)
	return hasTrue
}
