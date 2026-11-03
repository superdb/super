package expr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

// Test that Arith.Eval handles all ops for all vector forms.
func TestArithOpsAndForms(t *testing.T) {
	// These are all [0, 1, 2].
	lhsFlat := vector.NewInt(super.TypeInt64, []int64{0, 1, 2})
	lhsDict := vector.NewDict(lhsFlat, []byte{0, 1, 2}, nil)
	lhsView := vector.Pick(lhsFlat, []uint32{0, 1, 2})

	// These are all [1, 1, 1].
	rhsFlat := vector.NewInt(super.TypeInt64, []int64{1, 1, 1})
	rhsDict := vector.NewDict(rhsFlat, []byte{0, 0, 0}, nil)
	rhsView := vector.Pick(rhsFlat, []uint32{0, 1, 2})
	Const := vector.NewConstInt(super.TypeInt64, 1, 3)

	cases := []struct {
		op                            string
		expected, expectedForConstLHS []int64
	}{
		{"+", []int64{1, 2, 3}, []int64{2, 2, 2}},
		{"-", []int64{-1, 0, 1}, []int64{0, 0, 0}},
		{"*", []int64{0, 1, 2}, []int64{1, 1, 1}},
		{"/", []int64{0, 1, 2}, []int64{1, 1, 1}},
		{"%", []int64{0, 0, 0}, []int64{0, 0, 0}},
	}
	for _, c := range cases {
		f := func(expected []int64, lhs, rhs vector.Any) {
			t.Helper()
			cmp := NewArith(super.NewContext(), c.op, &testEval{lhs}, &testEval{rhs})
			assert.Equal(t, expected, cmp.Eval(nil).(*vector.Int).Values, "op: %s", c.op)
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

		// Arithmetic on two vector.Consts returns another vector.Const.
		cmp := NewArith(super.NewContext(), c.op, &testEval{Const}, &testEval{Const})
		val := cmp.Eval(nil).(*vector.Const)
		assert.Equal(t, uint32(3), val.Len(), "op: %s", c.op)
		expected := c.expectedForConstLHS[0]
		assert.Equal(t, expected, vector.IntValue(val, 0), "op: %s", c.op)
	}

}

// Test that a uint64 value too large for int64 yields an error in its slot
// when combined with a signed integer, and that the other slots still compute.
func TestArithUintOverflow(t *testing.T) {
	uints := vector.NewUint(super.TypeUint64, []uint64{5, 1 << 63, 7})
	ints := vector.NewConstInt(super.TypeInt64, 1, 3)
	for _, swap := range []bool{false, true} {
		lhs, rhs := vector.Any(uints), vector.Any(ints)
		if swap {
			lhs, rhs = rhs, lhs
		}
		out := NewArith(super.NewContext(), "+", &testEval{lhs}, &testEval{rhs}).Eval(nil).(*vector.Dynamic)
		assert.Equal(t, uint32(3), out.Len())
		assert.Equal(t, super.TypeInt64, out.TypeOf(0))
		assert.IsType(t, &super.TypeError{}, out.TypeOf(1))
		assert.Equal(t, super.TypeInt64, out.TypeOf(2))
	}
}
