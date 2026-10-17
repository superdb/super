package expr

//go:generate go run gencomparefuncs.go

import (
	"fmt"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/sam/expr/coerce"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

type Compare struct {
	sctx   *super.Context
	defuse *Defuse
	opCode int
	lhs    Evaluator
	rhs    Evaluator
	op     string
}

func NewCompare(sctx *super.Context, op string, lhs, rhs Evaluator) *Compare {
	return &Compare{sctx, NewDefuse(sctx), vector.CompareOpFromString(op), lhs, rhs, op}
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
	//XXX this shoud be check for missings
	if vec, ok := CheckForErrorThenNull(c.sctx, vecs, c.op); ok {
		return vec
	}
	lhs := vector.Under(vector.Super(vecs[0]))
	rhs := vector.Under(vector.Super(vecs[1]))
	//XXX missings should raise an error
	if which, ok := compareNones(lhs, rhs); ok {
		if c.opCode == vector.CompNE {
			which = !which
		}
		return vector.NewConstBool(which, lhs.Len())
	}
	lhs, rhs, errVal := coerceVals(c.sctx, lhs, rhs)
	if errVal != nil {
		// Incompatible types so return true for != and false otherwise.
		return vector.NewConstBool(c.opCode == vector.CompNE, errVal.Len())
	}
	//XXX need to handle overflow (see sam)
	kind := lhs.Kind()
	if kind != rhs.Kind() {
		panic("vector kind mismatch after coerce")
	}
	switch kind {
	case vector.KindBool:
		return c.compareBool(lhs, rhs)
	case vector.KindIP:
		return c.compareIPs(lhs, rhs)
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

func (c *Compare) compareNets(lhs, rhs vector.Any) vector.Any {
	if c.opCode != vector.CompEQ && c.opCode != vector.CompNE {
		s := fmt.Sprintf("type net incompatible with '%s' operator", vector.CompareOpToString(c.opCode))
		return vector.NewStringError(c.sctx, s, lhs.Len())
	}
	out := vector.NewFalse(lhs.Len())
	for i := range lhs.Len() {
		l := vector.NetValue(lhs, i)
		r := vector.NetValue(rhs, i)
		set := l == r
		if c.opCode == vector.CompNE {
			set = !set
		}
		if set {
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
	if c.opCode == vector.CompLT || c.opCode == vector.CompGT ||
		lhs.Type().ID() != rhs.Type().ID() {
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
