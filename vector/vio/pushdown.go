package vio

import (
	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type Pushdown interface {
	Projection() field.Projection
	SearchFilter() (Searcher, error)
	MetaFilter() (*ValueFilter, error)
	DataFilter() (*Filter, error)
	Unordered() bool
}

type Evaluator interface {
	Eval(vector.Any) vector.Any
}

type ValueEvaluator interface {
	Eval(super.Value) super.Value
}

type Searcher interface {
	Eval(string) bool
}

type Filter struct {
	Expr       Evaluator
	Projection field.Projection
}

type ValueFilter struct {
	Expr       ValueEvaluator
	Projection field.Projection
}
