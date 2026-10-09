package vio

import (
	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type Evaluator interface {
	Eval(vector.Any) vector.Any
}

type ValueEvaluator interface {
	Eval(super.Value) super.Value
}

type Pushdown interface {
	Projection() field.Projection
	// coming soon
	DataFilter() (Evaluator, error)
	//BSUPFilter() (*expr.BufferFilter, error)
	MetaFilter() (ValueEvaluator, field.Projection, error)
	// Undordered reports whether a reader may return values in arbirary order.
	Unordered() bool
}
