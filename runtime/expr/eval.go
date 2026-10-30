package expr

import (
	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type Evaluator interface {
	Eval(vector.Any) vector.Any
}

type Function interface {
	Call(...vector.Any) vector.Any
}

const (
	CatchOption    = 1
	CatchNone      = 2
	CatchError     = 4
	CatchNull      = 8
	CatchWrapError = 16
	CatchAll       = CatchOption | CatchNone | CatchError | CatchNull | CatchWrapError
	CatchBasic     = CatchError | CatchNull | CatchWrapError
)

func CatchFirst(sctx *super.Context, opts int, vecs []vector.Any, msg string) (vector.Any, bool) {
	if nullOrErr := opts & (CatchError | CatchNull | CatchWrapError); nullOrErr != 0 {
		if catchErr := nullOrErr & (CatchError | CatchWrapError); catchErr != 0 {
			for _, vec := range vecs {
				if vec, ok := Catch(sctx, catchErr, vec, msg); ok {
					return vec, true
				}
			}
		}
		if catchNull := nullOrErr & CatchNull; catchNull != 0 {
			for _, vec := range vecs {
				if vec, ok := Catch(sctx, catchNull, vec, msg); ok {
					return vec, true
				}
			}
		}
	}
	if opts &= ^(CatchError | CatchNull | CatchWrapError); opts != 0 {
		for _, vec := range vecs {
			if vec, ok := Catch(sctx, opts, vec, msg); ok {
				return vec, true
			}
		}
	}
	return nil, false
}

func Catch(sctx *super.Context, opts int, vec vector.Any, msg string) (vector.Any, bool) {
	switch vec.Kind() {
	case vector.KindError:
		if opts&CatchError != 0 {
			if opts&CatchWrapError != 0 {
				//XXX change this to error encountered or something better
				return wrapErr(sctx, msg, "error value encountered", vec), true
			}
			return vec, true
		}
	case vector.KindMissing:
		return catchErr(sctx, msg, "unexpected missing value", vec), true
	case vector.KindNone:
		if opts&CatchNone != 0 {
			return catchErr(sctx, msg, "none value not handled (consider ??)", vec), true
		}
	case vector.KindNull:
		if opts&CatchNull != 0 {
			return vec, true
		}
	case vector.KindOption:
		if opts&CatchOption != 0 {
			return catchErr(sctx, msg, "option value not handled (consider ??)", vec), true
		}
	}
	return nil, false
}

func catchErr(sctx *super.Context, prefix, msg string, vec vector.Any) *vector.Error {
	if prefix != "" {
		prefix += ": "
	}
	return vector.NewStringError(sctx, prefix+msg, vec.Len())
}

func wrapErr(sctx *super.Context, prefix, msg string, vec vector.Any) *vector.Error {
	if prefix != "" {
		prefix += ": "
	}
	return vector.NewWrappedError(sctx, prefix+msg, vec)
}

type Call struct {
	fn       Function
	defuse   *SuperDefuse
	exprs    []Evaluator
	applyOpt vector.ApplyOpt
	args     []vector.Any
}

func NewCall(sctx *super.Context, fn Function, exprs []Evaluator) *Call {
	opt := vector.ApplyRipUnions
	if fn, ok := fn.(interface{ ApplyOpt() vector.ApplyOpt }); ok {
		opt = fn.ApplyOpt()
	}
	defuse := NewSuperDefuse(sctx)
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
