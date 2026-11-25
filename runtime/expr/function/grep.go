package function

import (
	"regexp"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
	"golang.org/x/text/unicode/norm"
)

type Grep struct {
	sctx    *super.Context
	search  expr.Evaluator
	err     error
	pattern string
	cached  bool
}

func (g *Grep) Call(args ...vector.Any) vector.Any {
	patternVec, inputVec := args[0], args[1]
	if patternVec.Type().ID() != super.IDString {
		return vector.NewWrappedError(g.sctx, "grep: pattern argument must be a string", patternVec)
	}
	if inputVec.Len() == 0 {
		return vector.NewFalse(0)
	}
	if vec, ok := expr.Catch(g.sctx, expr.CatchAll, patternVec, "grep"); ok {
		return vec
	}
	if vec, ok := expr.Catch(g.sctx, expr.CatchAll, inputVec, "grep"); ok {
		return vec
	}
	if _, ok := vector.Under(patternVec).(*vector.Const); ok {
		if err := g.compile(vector.StringValue(patternVec, 0)); err != nil {
			return vector.NewWrappedError(g.sctx, regexpErrMsg("grep", err), patternVec)
		}
		return g.search.Eval(inputVec)
	}
	var index [1]uint32
	errMsg := vector.NewStringEmpty(0)
	var errs, matches []uint32
	for i := range patternVec.Len() {
		if err := g.compile(vector.StringValue(patternVec, i)); err != nil {
			errMsg.Append(regexpErrMsg("grep", err))
			errs = append(errs, i)
			continue
		}
		index[0] = i
		v := g.search.Eval(vector.Pick(inputVec, index[:]))
		if vector.BoolValue(v, 0) {
			matches = append(matches, i-uint32(len(errs)))
		}
	}
	out := vector.NewFalse(patternVec.Len() - uint32(len(errs)))
	for _, k := range matches {
		out.Set(k)
	}
	if len(errs) == 0 {
		return out
	}
	c := vector.NewCombiner(out)
	c.Add(errs, vector.NewVecWrappedError(g.sctx, errMsg, vector.Pick(patternVec, errs)))
	return c.Result()
}

func (g *Grep) compile(pattern string) error {
	if g.cached && g.pattern == pattern {
		return g.err
	}
	g.pattern, g.cached = pattern, true
	re, err := regexp.Compile(norm.NFC.String(pattern))
	if err != nil {
		g.search, g.err = nil, err
		return err
	}
	g.search, g.err = expr.NewSearchRegexp(g.sctx, re, &expr.This{}), nil
	return nil
}
