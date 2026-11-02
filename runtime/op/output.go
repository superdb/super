package op

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// Output is the puller for an output of a query.  It turns any plain none
// values into errors.  A plain none comes from an operator like ok() or ?.
// that handles a none so it can disappear from a record, array, or map, but
// a query must not output one (it should be handled with ??).
type Output struct {
	sctx   *super.Context
	parent vio.Puller
}

func NewOutput(sctx *super.Context, parent vio.Puller) *Output {
	return &Output{sctx: sctx, parent: parent}
}

func (o *Output) Pull(done bool) (vector.Any, error) {
	vec, err := o.parent.Pull(done)
	if vec == nil || err != nil {
		return vec, err
	}
	return o.noneToError(vec), nil
}

const noneErr = "none not handled (consider ??)"

func (o *Output) noneToError(vec vector.Any) vector.Any {
	switch v := vec.(type) {
	case *vector.None:
		return vector.NewStringError(o.sctx, noneErr, v.Len())
	case *vector.Dynamic:
		// The values of a dynamic may themselves be dynamics.
		var vals []vector.Any
		for k, val := range v.Values {
			if val == nil {
				continue
			}
			if out := o.noneToError(val); out != val {
				if vals == nil {
					vals = slices.Clone(v.Values)
				}
				vals[k] = out
			}
		}
		if vals == nil {
			return vec
		}
		return vector.NewDynamic(v.Tags, vals)
	case *vector.Union:
		if slices.ContainsFunc(v.Values(), isNoneVec) {
			return o.noneToError(v.Dynamic())
		}
	case *vector.Fusion:
		// A fusion value is a plain none if its subtype is none.
		var index, rest []uint32
		for i, typ := range v.Subtypes.Types() {
			if typ == super.TypeNone {
				index = append(index, uint32(i))
			} else {
				rest = append(rest, uint32(i))
			}
		}
		if len(index) == 0 {
			return vec
		}
		errs := vector.NewStringError(o.sctx, noneErr, uint32(len(index)))
		if len(rest) == 0 {
			return errs
		}
		return vector.Combine(vector.Pick(vec, rest), index, errs)
	case *vector.View:
		if isNoneVec(v.Any) {
			return vector.NewStringError(o.sctx, noneErr, v.Len())
		}
	}
	return vec
}

func isNoneVec(vec vector.Any) bool {
	return vec != nil && vec.Kind() == vector.KindNone
}
