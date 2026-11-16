package expr

//go:generate go run gencomparefuncs.go

import (
	"cmp"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr/cast"
	"github.com/superdb/super/runtime/sam/expr/coerce"
	samfunc "github.com/superdb/super/runtime/sam/expr/function"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

type Compare struct {
	sctx    *super.Context
	defuse  *SuperDefuse
	opCode  int
	lhs     Evaluator
	rhs     Evaluator
	op      string
	lenient bool

	hasFusion samfunc.FusionChecker
	// comparisonTypes caches coerce.ComparisonType for pairs of operand
	// types.
	comparisonTypes map[[2]super.Type]comparisonType
}

type comparisonType struct {
	typ super.Type
	ok  bool
}

// NewCompare returns an evaluator for comparison operator op, for which
// values whose types have no coerce.ComparisonType are an error.
func NewCompare(sctx *super.Context, op string, lhs, rhs Evaluator) *Compare {
	return &Compare{sctx: sctx, defuse: NewSuperDefuse(sctx), opCode: vector.CompareOpFromString(op), lhs: lhs, rhs: rhs, op: op}
}

// NewLenientCompare is like NewCompare but values whose types can't be
// compared are unequal rather than an error, as for the in operator.
func NewLenientCompare(sctx *super.Context, op string) *Compare {
	c := NewCompare(sctx, op, nil, nil)
	c.lenient = true
	return c
}

func (c *Compare) Compare(vec0, vec1 vector.Any) vector.Any {
	return c.eval(vec0, vec1)
}

func (c *Compare) Eval(val vector.Any) vector.Any {
	lhs := c.defuse.Eval(c.lhs.Eval(val))
	rhs := c.defuse.Eval(c.rhs.Eval(val))
	return vector.Apply(vector.ApplyRipUnions, c.eval, lhs, rhs)
}

func (c *Compare) eval(vecs ...vector.Any) vector.Any {
	if vecs[0].Kind() == vector.KindOption && vecs[1].Kind() == vector.KindOption {
		return c.compareOptions(vecs[0], vecs[1])
	}
	// An error or null operand takes precedence over an unhandled option.
	if vec, ok := CatchFirst(c.sctx, CatchBasic|CatchOption, vecs, c.op); ok {
		return vec
	}
	return c.evalCaught(vecs...)
}

func (c *Compare) evalAllowingOption(vecs ...vector.Any) vector.Any {
	if vec, ok := CatchFirst(c.sctx, CatchBasic, vecs, c.op); ok {
		return vec
	}
	return c.evalCaught(vecs...)
}

// evalCaught is evalAllowingOption for operands already checked for errors
// and nulls.
func (c *Compare) evalCaught(vecs ...vector.Any) vector.Any {
	if vecs[0].Kind() == vector.KindOption || vecs[1].Kind() == vector.KindOption {
		return c.compareOptions(vecs[0], vecs[1])
	}
	lhs, rhs := vector.Super(vecs[0]), vector.Super(vecs[1])
	ltyp, rtyp := lhs.Type(), rhs.Type()
	lhs, rhs = vector.Under(lhs), vector.Under(rhs)
	if which, ok := compareNones(lhs, rhs); ok {
		if c.opCode == vector.CompNE {
			which = !which
		}
		return vector.NewConstBool(which, lhs.Len())
	}
	if vec, ok := c.compareNone(lhs, rhs); ok {
		return vec
	}
	if vec, ok := c.compareMixedInts(lhs, rhs); ok {
		return vec
	}
	lhs, rhs, ok, defused := c.coerce(lhs, rhs)
	if defused {
		return vector.Apply(vector.ApplyRipUnions, c.evalAllowingOption, lhs, rhs)
	}
	if !ok {
		if c.lenient {
			return vector.NewConstBool(c.opCode == vector.CompNE, lhs.Len())
		}
		return vector.NewStringError(c.sctx, coerce.TypeMismatch(c.op, ltyp, rtyp), lhs.Len())
	}
	kind := lhs.Kind()
	if kind != rhs.Kind() || kind == vector.KindError {
		// A value couldn't be cast to the comparison type (e.g., a uint64
		// too large for int64), leaving an error in its place, so it's
		// unequal, as when a nested value couldn't be cast (see
		// compareComplex).
		if lhs.Kind() == vector.KindError || rhs.Kind() == vector.KindError {
			return vector.NewConstBool(c.opCode == vector.CompNE, lhs.Len())
		}
		_, lok := lhs.(*vector.Dynamic)
		_, rok := rhs.(*vector.Dynamic)
		if !lok && !rok {
			panic("vector kind mismatch after coerce")
		}
		// Some values couldn't be cast, so handle each part as above.
		return vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
			if vecs[0].Kind() == vector.KindError || vecs[1].Kind() == vector.KindError || vecs[0].Type() != vecs[1].Type() {
				return vector.NewConstBool(c.opCode == vector.CompNE, vecs[0].Len())
			}
			return c.evalAllowingOption(vecs...)
		}, lhs, rhs)
	}
	switch kind {
	case vector.KindBool:
		return c.compareBool(lhs, rhs)
	case vector.KindIP:
		return c.compareIPs(lhs, rhs)
	case vector.KindUUID:
		return c.compareUUIDs(lhs, rhs)
	case vector.KindNet:
		return c.compareNets(lhs, rhs)
	case vector.KindType:
		return c.compareTypeVals(lhs, rhs)
	case vector.KindRecord,
		vector.KindArray,
		vector.KindSet,
		vector.KindMap,
		vector.KindEnum:
		return c.compareComplex(lhs, rhs)
	}
	return c.compareForms(kind, lhs, rhs)
}

func (c *Compare) compareForms(kind vector.Kind, lhs, rhs vector.Any) vector.Any {
	lform, ok := vector.FormOf(lhs)
	if !ok {
		return vector.NewStringError(c.sctx, coerce.ErrIncompatibleTypes.Error(), lhs.Len())
	}
	rform, ok := vector.FormOf(rhs)
	if !ok {
		return vector.NewStringError(c.sctx, coerce.ErrIncompatibleTypes.Error(), lhs.Len())
	}
	f, ok := compareFuncs[vector.FuncCode(c.opCode, kind, lform, rform)]
	if !ok {
		return vector.NewConstBool(false, lhs.Len())
	}
	return f(lhs, rhs)
}

// compareOptions compares the payloads of some values while an option none
// equals only another option none.  An option is never equal to a non-option.
func (c *Compare) compareOptions(lhs, rhs vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipOptions, func(vecs ...vector.Any) vector.Any {
		if vecs[0].Kind() != vector.KindOption || vecs[1].Kind() != vector.KindOption {
			return vector.NewConstBool(c.opCode == vector.CompNE, vecs[0].Len())
		}
		lhs := vector.UnderOption(vecs[0]).Any
		rhs := vector.UnderOption(vecs[1]).Any
		if vec, ok := c.compareNone(lhs, rhs); ok {
			return vec
		}
		return vector.Apply(vector.ApplyRipFusions|vector.ApplyRipUnions, c.evalAllowingOption, lhs, rhs)
	}, lhs, rhs)
}

// compareNone compares lhs and rhs if either is none, which equals only
// none and has no order.
func (c *Compare) compareNone(lhs, rhs vector.Any) (vector.Any, bool) {
	lhsNone, rhsNone := lhs.Kind() == vector.KindNone, rhs.Kind() == vector.KindNone
	if !lhsNone && !rhsNone {
		return nil, false
	}
	eq := lhsNone && rhsNone
	return vector.NewConstBool(c.opCode == vector.CompEQ && eq || c.opCode == vector.CompNE && !eq, lhs.Len()), true
}

// compareMixedInts compares an unsigned integer with a signed integer that
// is no wider, for which the signed type they would otherwise be cast to
// can't hold every unsigned value (e.g., a uint64 greater than the maximum
// int64).  It returns false for any other types.
func (c *Compare) compareMixedInts(lhs, rhs vector.Any) (vector.Any, bool) {
	isUnsigned := func(id int) bool { return id >= super.IDUint8 && id <= super.IDUint64 }
	isSigned := func(id int) bool { return id >= super.IDInt8 && id <= super.IDInt64 }
	lid, rid := lhs.Type().ID(), rhs.Type().ID()
	var flip bool
	switch {
	case isUnsigned(lid) && isSigned(rid):
	case isSigned(lid) && isUnsigned(rid):
		lhs, rhs, flip = rhs, lhs, true
	default:
		return nil, false
	}
	if coerce.UintBits(lhs.Type()) < coerce.IntBits(rhs.Type()) {
		return nil, false
	}
	n := lhs.Len()
	if _, ok := rhs.(*vector.Const); ok && n > 0 && vector.IntValue(rhs, 0) >= 0 {
		// The signed constant fits the unsigned type, so use the
		// vectorized comparison.
		rhs = cast.To(c.sctx, rhs, lhs.Type())
		if flip {
			lhs, rhs = rhs, lhs
		}
		return c.compareForms(vector.KindUint, lhs, rhs), true
	}
	out := vector.NewFalse(n)
	for i := range n {
		u, s := vector.UintValue(lhs, i), vector.IntValue(rhs, i)
		v := 1
		if s >= 0 {
			v = cmp.Compare(u, uint64(s))
		}
		if flip {
			v = -v
		}
		if isCompareOpSatisfied(c.opCode, v) {
			out.Set(i)
		}
	}
	return out, true
}

// coerce returns lhs and rhs cast as needed to their comparison type or false
// if their types can't be compared.  If their types contain fusion types that
// leave the comparison type unknown, it instead returns them defused, with
// defused true, to be compared again.
func (c *Compare) coerce(lhs, rhs vector.Any) (vector.Any, vector.Any, bool, bool) {
	ltyp, rtyp := lhs.Type(), rhs.Type()
	if ltyp == rtyp {
		return lhs, rhs, true, false
	}
	key := [2]super.Type{ltyp, rtyp}
	ct, cached := c.comparisonTypes[key]
	if !cached {
		ct.typ, ct.ok = coerce.ComparisonType(c.sctx, c.hasFusion.Check, ltyp, rtyp)
		if c.comparisonTypes == nil {
			c.comparisonTypes = make(map[[2]super.Type]comparisonType)
		}
		c.comparisonTypes[key] = ct
	}
	typ, ok := ct.typ, ct.ok
	if !ok {
		return lhs, rhs, false, false
	}
	if coerce.IsUnknown(typ) {
		if c.hasFusion.Check(ltyp) || c.hasFusion.Check(rtyp) {
			lhs, rhs := c.defuse.Eval(lhs), c.defuse.Eval(rhs)
			// Compare again only if defusing removed the fusion types so
			// this can't recur forever.
			if c.defused(lhs) && c.defused(rhs) {
				return lhs, rhs, true, true
			}
		}
		return lhs, rhs, false, false
	}
	to := cast.To
	if typ.Kind() != super.PrimitiveKind {
		to = cast.Cast
	}
	if ltyp != typ {
		lhs = to(c.sctx, lhs, typ)
	}
	if rtyp != typ {
		rhs = to(c.sctx, rhs, typ)
	}
	return vector.Under(lhs), vector.Under(rhs), true, false
}

// defused reports whether vec, the result of defusing a vector, has no fusion
// types (or is a Dynamic, whose parts are checked when compared).
func (c *Compare) defused(vec vector.Any) bool {
	return vector.IsDynamic(vec) || !c.hasFusion.Check(vec.Type())
}

func compareNones(a, b vector.Any) (bool, bool) {
	if a.Kind() == vector.KindMissing {
		if b.Kind() == vector.KindMissing {
			return true, true
		}
		return false, true
	}
	if b.Kind() == vector.KindMissing {
		return false, true
	}
	return false, false
}

func (c *Compare) compareBool(lhs, rhs vector.Any) vector.Any {
	if c.opCode == vector.CompLT || c.opCode == vector.CompGT {
		return vector.NewConstBool(false, lhs.Len())
	}
	out := vector.NewFalse(lhs.Len())
	if lhs, ok := lhs.(*vector.Bool); ok {
		if rhs, ok := rhs.(*vector.Bool); ok {
			lhsBits := lhs.GetBits()
			rhsBits := rhs.GetBits()
			outBits := out.GetBits()
			for i := range outBits {
				// XOR is bit-wise "not equal".
				outBits[i] = lhsBits[i] ^ rhsBits[i]
				if c.opCode != vector.CompNE {
					outBits[i] = ^outBits[i]
				}
			}
			return out
		}
	}
	for i := range lhs.Len() {
		l := vector.BoolValue(lhs, i)
		r := vector.BoolValue(rhs, i)
		v := l == r
		if c.opCode == vector.CompNE {
			v = !v
		}
		if v {
			out.Set(i)
		}
	}
	return out
}

func (c *Compare) compareIPs(lhs, rhs vector.Any) vector.Any {
	out := vector.NewFalse(lhs.Len())
	for i := range lhs.Len() {
		l := vector.IPValue(lhs, i)
		r := vector.IPValue(rhs, i)
		if isCompareOpSatisfied(c.opCode, l.Compare(r)) {
			out.Set(i)
		}
	}
	return out
}

func (c *Compare) compareUUIDs(lhs, rhs vector.Any) vector.Any {
	out := vector.NewFalse(lhs.Len())
	for i := range lhs.Len() {
		if isCompareOpSatisfied(c.opCode, vector.UUIDValue(lhs, i).Compare(vector.UUIDValue(rhs, i))) {
			out.Set(i)
		}
	}
	return out
}

func (c *Compare) compareNets(lhs, rhs vector.Any) vector.Any {
	out := vector.NewFalse(lhs.Len())
	for i := range lhs.Len() {
		l := vector.NetValue(lhs, i)
		r := vector.NetValue(rhs, i)
		if isCompareOpSatisfied(c.opCode, l.Compare(r)) {
			out.Set(i)
		}
	}
	return out
}

func isCompareOpSatisfied(opCode, i int) bool {
	switch opCode {
	case vector.CompLT:
		return i < 0
	case vector.CompLE:
		return i <= 0
	case vector.CompGT:
		return i > 0
	case vector.CompGE:
		return i >= 0
	case vector.CompEQ:
		return i == 0
	case vector.CompNE:
		return i != 0
	}
	panic(opCode)
}

func (c *Compare) compareTypeVals(lhs, rhs vector.Any) vector.Any {
	if c.opCode == vector.CompLT || c.opCode == vector.CompGT {
		return vector.NewConstBool(false, lhs.Len())
	}
	out := vector.NewFalse(lhs.Len())
	for i := range lhs.Len() {
		l := vector.TypeValueValue(lhs, i)
		r := vector.TypeValueValue(rhs, i)
		v := l == r
		if c.opCode == vector.CompNE {
			v = !v
		}
		if v {
			out.Set(i)
		}
	}
	return out
}

func (c *Compare) compareComplex(lhs, rhs vector.Any) vector.Any {
	if lhs.Type() != rhs.Type() {
		// A nested value couldn't be cast to the comparison type (e.g., a
		// uint64 too large for int64), so the values are unequal.
		return vector.NewConstBool(c.opCode == vector.CompNE, lhs.Len())
	}
	if c.opCode == vector.CompLT || c.opCode == vector.CompGT {
		return vector.NewConstBool(false, lhs.Len())
	}
	var lb, rb scode.Builder
	out := vector.NewFalse(lhs.Len())
	for i := range lhs.Len() {
		l := vector.ValueAt(&lb, lhs, i).Bytes()
		r := vector.ValueAt(&rb, rhs, i).Bytes()
		v := string(l) == string(r)
		if c.opCode == vector.CompNE {
			v = !v
		}
		if v {
			out.Set(i)
		}
	}
	return out
}

type isNull struct {
	expr Evaluator
}

func NewIsNull(e Evaluator) Evaluator {
	return &isNull{e}
}

func (i *isNull) Eval(this vector.Any) vector.Any {
	return vector.Apply(vector.ApplyRipUnions|vector.ApplyRipFusions, i.eval, i.expr.Eval(this))
}

func (i *isNull) eval(vecs ...vector.Any) vector.Any {
	k := vecs[0].Kind()
	if k == vector.KindError {
		return vecs[0]
	}
	return vector.NewConstBool(k == vector.KindNull, vecs[0].Len())
}
