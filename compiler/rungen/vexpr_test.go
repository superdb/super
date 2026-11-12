package rungen

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superdb/super"
	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/expr"
)

// Test that "in" with an array, set, or record of literals compiles to
// expr.InConst and that anything else compiles to expr.In.
func TestCompileInConst(t *testing.T) {
	lit := func(s string) dag.Expr {
		return &dag.PrimitiveExpr{Kind: "PrimitiveExpr", Value: s}
	}
	cast := func(s, typ string) dag.Expr {
		return dag.NewCall("cast", []dag.Expr{lit(s), lit(typ)})
	}
	vals := func(exprs ...dag.Expr) []dag.VectorElem {
		var elems []dag.VectorElem
		for _, e := range exprs {
			elems = append(elems, &dag.VectorValue{Kind: "VectorValue", Expr: e})
		}
		return elems
	}
	this := dag.NewThis(field.NewChain("x"))
	cases := []struct {
		rhs     dag.Expr
		inConst bool
	}{
		{&dag.ArrayExpr{Kind: "ArrayExpr", Elems: vals(lit("1"), lit(`"a"`), lit("null"))}, true},
		{&dag.SetExpr{Kind: "SetExpr", Elems: vals(lit("1"), lit("2"))}, true},
		{&dag.RecordExpr{Kind: "RecordExpr", Elems: []dag.RecordElem{
			&dag.Field{Kind: "Field", Name: "c0", Value: lit("1")},
			&dag.Field{Kind: "Field", Name: "c1", Value: lit(`"a"`)},
		}}, true},
		{&dag.ArrayExpr{Kind: "ArrayExpr", Elems: vals(cast("1", "<uint8>"), cast(`"10.0.0.1"`, "<ip>"))}, true},
		{&dag.ArrayExpr{Kind: "ArrayExpr", Elems: vals(lit("1"), this)}, false},
		{&dag.ArrayExpr{Kind: "ArrayExpr", Elems: vals(lit("missing"))}, false},
		{&dag.ArrayExpr{Kind: "ArrayExpr"}, false},
		{this, false},
	}
	b := NewBuilder(runtime.NewContext(context.Background(), super.NewContext()), nil)
	for _, c := range cases {
		e, err := b.compileVamExpr(dag.NewBinaryExpr("in", this, c.rhs))
		require.NoError(t, err)
		_, ok := e.(*expr.InConst)
		assert.Equal(t, c.inConst, ok, "rhs %#v", c.rhs)
	}
}
