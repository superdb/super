package expr

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/sam/expr/coerce"
	"github.com/superdb/super/vector"
)

type testEval struct {
	val vector.Any
}

func (t *testEval) Eval(_ vector.Any) vector.Any {
	return t.val
}

// Test that Compare.Eval handles all ops for all vector forms.
func TestCompareOpsAndForms(t *testing.T) {
	// These are all [0, 1, 2].
	lhsFlat := vector.NewUint(super.TypeUint64, []uint64{0, 1, 2})
	lhsDict := vector.NewDict(lhsFlat, []byte{0, 1, 2}, nil)
	lhsView := vector.Pick(lhsFlat, []uint32{0, 1, 2})

	// These are all [1, 1, 1].
	rhsFlat := vector.NewUint(super.TypeUint64, []uint64{1, 1, 1})
	rhsDict := vector.NewDict(rhsFlat, []byte{0, 0, 0}, nil)
	rhsView := vector.Pick(rhsFlat, []uint32{0, 1, 2})
	Const := vector.NewConstUint(super.TypeUint64, 1, 3)

	cases := []struct {
		op, expected, expectedForConstLHS string
	}{
		{"==", "010", "111"},
		{"!=", "101", "000"},
		{"<", "100", "000"},
		{"<=", "110", "111"},
		{">", "001", "000"},
		{">=", "011", "111"},
	}
	for _, c := range cases {
		f := func(expected string, lhs, rhs vector.Any) {
			t.Helper()
			cmp := NewCompare(super.NewContext(), c.op, &testEval{lhs}, &testEval{rhs})
			assert.Equal(t, expected, cmp.Eval(nil).(*vector.Bool).Bits.String(), "op: %s", c.op)
		}

		f(c.expected, lhsFlat, rhsFlat)
		f(c.expected, lhsFlat, rhsDict)
		f(c.expected, lhsFlat, rhsView)
		f(c.expected, lhsFlat, Const)

		f(c.expected, lhsDict, rhsFlat)
		f(c.expected, lhsDict, rhsDict)
		f(c.expected, lhsDict, rhsView)
		f(c.expected, lhsDict, Const)

		f(c.expected, lhsView, rhsFlat)
		f(c.expected, lhsView, rhsDict)
		f(c.expected, lhsView, rhsView)
		f(c.expected, lhsView, Const)

		f(c.expectedForConstLHS, Const, rhsFlat)
		f(c.expectedForConstLHS, Const, rhsDict)
		f(c.expectedForConstLHS, Const, rhsView)

		// Comparing two vector.Consts yields another vector.Const.
		cmp := NewCompare(super.NewContext(), c.op, &testEval{Const}, &testEval{Const})
		val := cmp.Eval(nil).(*vector.Const)
		assert.Equal(t, uint32(3), val.Len(), "op: %s", c.op)
		expected := c.expectedForConstLHS == "111"
		assert.Equal(t, expected, vector.BoolValue(val, 0), "op: %s", c.op)
	}
}

// Test that the generated float comparisons (see floatCond in
// gencomparefuncs.go) order NaN as coerce.CompareFloats does.
func TestCompareFloatsMatchesGenerated(t *testing.T) {
	values := []float64{math.NaN(), math.Inf(-1), -1, math.Copysign(0, -1), 0, 1, math.Inf(1)}
	forms := func(v float64) map[vector.Form]vector.Any {
		flat := vector.NewFloat(super.TypeFloat64, []float64{v})
		return map[vector.Form]vector.Any{
			vector.FormFlat:  flat,
			vector.FormDict:  vector.NewDict(flat, []byte{0}, nil),
			vector.FormView:  vector.Pick(flat, []uint32{0}),
			vector.FormConst: vector.NewConst(flat, 1),
		}
	}
	ops := []int{vector.CompEQ, vector.CompNE, vector.CompLT, vector.CompLE, vector.CompGT, vector.CompGE}
	for _, a := range values {
		for _, b := range values {
			for _, op := range ops {
				expected := isCompareOpSatisfied(op, coerce.CompareFloats(a, b))
				for lform, lhs := range forms(a) {
					for rform, rhs := range forms(b) {
						f := compareFuncs[vector.FuncCode(op, vector.KindFloat, lform, rform)]
						actual := vector.BoolValue(f(lhs, rhs), 0)
						assert.Equal(t, expected, actual, "%v %s %v (%s, %s)", a, vector.CompareOpToString(op), b, lform, rform)
					}
				}
			}
		}
	}
}
