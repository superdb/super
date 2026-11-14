package expr

import (
	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/vector"
)

// Renamer renames one or more fields in a record.  See [expr.Renamer], on which
// it relies, for more detail.
//
// A union (named or not) is treated as the value of its member, so if Renamer
// renames a field of any member of a union that is the value or is on the path
// to a renamed field, the union is replaced by the values of its members, as
// for the type computed by the semantic checker.
type Renamer struct {
	sctx    *super.Context
	defuse  *SuperDefuse
	renamer *expr.Renamer
	// paths holds the source paths if they are static.
	paths []field.Path
}

func NewRenamer(sctx *super.Context, srcs, dsts []*expr.Lval) *Renamer {
	var paths []field.Path
	for _, src := range srcs {
		path, ok := src.Path()
		if !ok {
			paths = nil
			break
		}
		paths = append(paths, path)
	}
	return &Renamer{sctx, NewSuperDefuse(sctx), expr.NewRenamer(sctx, srcs, dsts), paths}
}

func (r *Renamer) Eval(vec vector.Any) vector.Any {
	return vector.Apply(vector.ApplyNone, r.eval, r.defuse.Eval(vec))
}

func (r *Renamer) eval(vecs ...vector.Any) vector.Any {
	vec := vecs[0]
	dynamic, ok := ripUnions(r.sctx, vec, r.paths).(*vector.Dynamic)
	if !ok {
		return r.rename(vec)
	}
	values := make([]vector.Any, len(dynamic.Values))
	var changed bool
	for k, val := range dynamic.Values {
		if val == nil {
			continue
		}
		values[k] = r.rename(val)
		if values[k] != val {
			changed = true
		}
	}
	if !changed {
		return vec
	}
	// Stitch removes the nil values.
	return vector.Stitch(vector.NewDynamic(dynamic.Tags, values))
}

// rename returns vec if no fields are renamed.
func (r *Renamer) rename(vec vector.Any) vector.Any {
	recVec, ok := vector.PushView(vector.Under(vec)).(*vector.Record)
	if !ok {
		return vec
	}
	val, err := r.renamer.EvalToValAndError(super.NewValue(vec.Type(), nil))
	if err != nil {
		return vector.NewWrappedError(r.sctx, err.Error(), vec)
	}
	if val.Type() == vec.Type() {
		return vec
	}
	return recVec.ChangeType(val.Type().(*super.TypeRecord))
}
