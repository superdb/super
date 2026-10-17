package function

import (
	"fmt"
	"regexp"
	"regexp/syntax"

	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

type Regexp struct {
	re    *regexp.Regexp
	restr string
	err   error
	sctx  *super.Context
}

func (r *Regexp) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(r.sctx, true, args, "regexp"); ok {
		return vec
	}
	args = underAll(args)
	regVec, inputVec := args[0], args[1]
	if regVec.Type().ID() != super.IDString {
		return vector.NewWrappedError(r.sctx, "regexp: string required for first arg", args[0])
	}
	if inputVec.Type().ID() != super.IDString {
		return vector.NewWrappedError(r.sctx, "regexp: string required for second arg", args[1])
	}
	errMsg := vector.NewStringEmpty(0)
	var errs []uint32
	inner := vector.NewStringEmpty(0)
	out := vector.NewArray(r.sctx.LookupTypeArray(super.TypeString), []uint32{0}, inner)
	for i := range regVec.Len() {
		re := vector.StringValue(regVec, i)
		if r.restr != re {
			r.restr = re
			r.re, r.err = regexp.Compile(r.restr)
		}
		if r.err != nil {
			errMsg.Append(regexpErrMsg("regexp", r.err))
			errs = append(errs, i)
			continue
		}
		s := vector.StringValue(inputVec, i)
		match := r.re.FindStringSubmatch(s)
		for _, b := range match {
			inner.Append(b)
		}
		out.Offsets = append(out.Offsets, inner.Len())
	}
	c := vector.NewCombiner(out)
	if len(errs) > 0 {
		c.Add(errs, vector.NewVecWrappedError(r.sctx, errMsg, vector.Pick(regVec, errs)))
	}
	return c.Result()
}

type RegexpReplace struct {
	sctx  *super.Context
	re    *regexp.Regexp
	restr string
	err   error
}

func (r *RegexpReplace) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(r.sctx, true, args, "regexp_replace"); ok {
		return vec
	}
	args = underAll(args)
	for _, vec := range args {
		if vec.Type().ID() != super.IDString {
			return vector.NewWrappedError(r.sctx, "regexp_replace: string arg required", vec)
		}
	}
	sVec := args[0]
	reVec := args[1]
	replaceVec := args[2]
	errMsg := vector.NewStringEmpty(0)
	var errs []uint32
	out := vector.NewStringEmpty(0)
	for i := range sVec.Len() {
		s := vector.StringValue(sVec, i)
		re := vector.StringValue(reVec, i)
		replace := vector.StringValue(replaceVec, i)
		if r.restr != re {
			r.restr = re
			r.re, r.err = regexp.Compile(re)
		}
		if r.err != nil {
			errMsg.Append(regexpErrMsg("regexp_replace", r.err))
			errs = append(errs, i)
			continue
		}
		out.Append(r.re.ReplaceAllString(s, replace))
	}
	if len(errs) > 0 {
		return vector.Combine(out, errs, vector.NewVecWrappedError(r.sctx, errMsg, vector.Pick(args[1], errs)))
	}
	return out
}

func regexpErrMsg(fn string, err error) string {
	msg := fmt.Sprintf("%s: invalid regular expression", fn)
	if syntaxErr, ok := err.(*syntax.Error); ok {
		msg += ": " + syntaxErr.Code.String()
	}
	return msg
}
