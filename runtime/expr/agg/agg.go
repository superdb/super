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
			return newArrayAgg(sctx)
		}
	case "fuse":
		pattern = func() expr.AggFunc {
			return newFuse(sctx, false)
		}
	case "dcount":
		pattern = func() expr.AggFunc {
			return newDCount()
		}
	case "superfuse":
		pattern = func() expr.AggFunc {
			return newFuse(sctx, true)
		}
	case "sum":
		pattern = func() expr.AggFunc {
			return newSum()
		}
	case "min":
		pattern = func() expr.AggFunc {
			return newMin()
		}
	case "max":
		pattern = func() expr.AggFunc {
			return newMax()
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
			return newAnd()
		}
	case "or":
		pattern = func() expr.AggFunc {
			return newOr()
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

func pick[E any](s []E, index []uint32) []E {
	if index == nil {
		return s
	}
	out := make([]E, len(index))
	for i, k := range index {
		out[i] = s[k]
	}
	return out
}
