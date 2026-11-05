package expr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/superdb/super"
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

// Test that comparing signed and unsigned integers is exact, including
// uint64 values too large for int64, which coercion to int64 cannot represent.
func TestCompareSignedUnsignedOverflow(t *testing.T) {
	ints := vector.NewInt(super.TypeInt64, []int64{-1, 5, 5, 5})
	uints := vector.NewUint(super.TypeUint64, []uint64{1<<64 - 1, 5, 1 << 63, 4})
	cases := []struct {
		op, expected, expectedSwapped string
	}{
		{"==", "0100", "0100"},
		{"!=", "1011", "1011"},
		{"<", "1010", "0001"},
		{"<=", "1110", "0101"},
		{">", "0001", "1010"},
		{">=", "0101", "1110"},
	}
	for _, c := range cases {
		cmp := NewCompare(super.NewContext(), c.op, &testEval{ints}, &testEval{uints})
		assert.Equal(t, c.expected, cmp.Eval(nil).(*vector.Bool).Bits.String(), "int %s uint", c.op)
		cmp = NewCompare(super.NewContext(), c.op, &testEval{uints}, &testEval{ints})
		assert.Equal(t, c.expectedSwapped, cmp.Eval(nil).(*vector.Bool).Bits.String(), "uint %s int", c.op)
	}
}
