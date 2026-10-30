package agg

import (
	"fmt"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
)

func NewPattern(sctx *super.Context, op string, distinct, hasarg bool) (expr.AggPattern, error) {
	needarg := true
	var pattern expr.AggPattern
	switch op {
	case "count":
		needarg = false
		pattern = func() expr.AggFunc {
			return &count{}
		}
	case "any":
		pattern = func() expr.AggFunc {
			return NewAny()
		}
	case "avg":
		pattern = func() expr.AggFunc {
			return &avg{}
		}
	case "array_agg":
		pattern = func() expr.AggFunc {
			return &arrayAgg{*newCollect(sctx)}
		}
	case "fuse":
		pattern = func() expr.AggFunc {
			return newFuse(false)
		}
	case "dcount":
		pattern = func() expr.AggFunc {
			return newDCount()
		}
	case "superfuse":
		pattern = func() expr.AggFunc {
			return newFuse(true)
		}
	case "sum":
		pattern = func() expr.AggFunc {
			return newMathReducer(mathSum)
		}
	case "min":
		pattern = func() expr.AggFunc {
			return newMathReducer(mathMin)
		}
	case "max":
		pattern = func() expr.AggFunc {
			return newMathReducer(mathMax)
		}
	case "union":
		pattern = func() expr.AggFunc {
			return newUnion()
		}
	case "collect":
		pattern = func() expr.AggFunc {
			return newCollect(sctx)
		}
	case "collect_map":
		pattern = func() expr.AggFunc {
			return newCollectMap()
		}
	case "and":
		pattern = func() expr.AggFunc {
			return &and{}
		}
	case "or":
		pattern = func() expr.AggFunc {
			return &or{}
		}
	default:
		return nil, fmt.Errorf("unknown aggregation function: %s", op)
	}
	if needarg && !hasarg {
		return nil, fmt.Errorf("%s: argument required", op)
	}
	if distinct {
		switch op {
		case "array_agg", "avg", "collect", "count", "sum":
			// Distinct affects only these functions.
			return func() expr.AggFunc {
				return newDistinct(pattern())
			}, nil
		}
	}
	return pattern, nil
}
