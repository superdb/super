package expr

import (
	"fmt"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type Evaluator interface {
	Eval(vector.Any) vector.Any
}

type Function interface {
	Call(...vector.Any) vector.Any
}

// CheckForErrorThenNull returns the first element of vecs with an error
// type. If no element has an error type, it returns the first element with the
// null type.
func CheckForErrorThenNull(sctx *super.Context, vecs []vector.Any, msg string) (vector.Any, bool) {
	for _, vec := range vecs {
		if vec.Kind() == vector.KindError {
			return vector.NewWrappedError(sctx, fmt.Sprintf("%s: error value encountered", msg), vec), true
		}
	}
	for _, vec := range vecs {
		if vec.Kind() == vector.KindNull {
			return vec, true
		}
	}
	return nil, false
}

func Catch(sctx *super.Context, options bool, vecs []vector.Any, msg string) (vector.Any, bool) {
	if vec, ok := CheckForErrorThenNull(sctx, vecs, msg); ok {
		return vec, true
	}
	for _, vec := range vecs {
		if vec.Kind() == vector.KindMissing {
			return vector.NewStringError(sctx, fmt.Sprintf("%s: missing value not handled (consider ??)", msg), vec.Len()), true
		}
		if options && vec.Kind() == vector.KindOption {
			return vector.NewStringError(sctx, fmt.Sprintf("%s: option value not handled (consider ??)", msg), vec.Len()), true
		}
	}
	return nil, false
}

type Call struct {
	fn       Function
	defuse   *Defuse
	exprs    []Evaluator
	applyOpt vector.ApplyOpt
	args     []vector.Any
}

func NewCall(sctx *super.Context, fn Function, exprs []Evaluator) *Call {
	opt := vector.ApplyRipUnions
	if fn, ok := fn.(interface{ ApplyOpt() vector.ApplyOpt }); ok {
		opt = fn.ApplyOpt()
	}
	defuse := NewDefuse(sctx)
	if fn, ok := fn.(interface{ NoDefuse() bool }); ok {
		if fn.NoDefuse() {
			defuse = nil
		}
	}
	return &Call{
		fn:       fn,
		defuse:   defuse,
		exprs:    exprs,
		applyOpt: opt,
		args:     make([]vector.Any, len(exprs)),
	}
}

func (c *Call) Eval(this vector.Any) vector.Any {
	for k, e := range c.exprs {
		vec := e.Eval(this)
		if c.defuse != nil {
			vec = c.defuse.Eval(vec)
		}
		c.args[k] = vec
	}
	return vector.Apply(c.applyOpt, c.fn.Call, c.args...)
}
