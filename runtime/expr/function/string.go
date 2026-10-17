package function

import (
	"strings"

	"github.com/agnivade/levenshtein"
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

type Concat struct {
	sctx *super.Context
}

func (c *Concat) Call(args ...vector.Any) vector.Any {
	n := args[0].Len()
	vecs := args[:0]
	for _, vec := range args {
		switch vec.Kind() {
		case vector.KindString:
			vecs = append(vecs, vec)
		case vector.KindNull:
			// Ignored.
		case vector.KindError:
			return vec
		default:
			return vector.NewWrappedError(c.sctx, "concat: string arg required", vec)
		}
	}
	if len(vecs) == 0 {
		return vector.NewConstString("", n)
	}
	out := vector.NewStringEmpty(0)
	for i := range n {
		var b strings.Builder
		for _, vec := range vecs {
			s := vector.StringValue(vec, i)
			b.WriteString(s)
		}
		out.Append(b.String())
	}
	return out
}

type Join struct {
	sctx *super.Context
}

func (j *Join) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(j.sctx, true, args, "join"); ok {
		return vec
	}
	args = underAll(args)
	splitsVal := args[0]
	typ, ok := splitsVal.Type().(*super.TypeArray)
	if !ok || typ.Type.ID() != super.IDString {
		return vector.NewWrappedError(j.sctx, "join: array of string arg required", splitsVal)
	}
	var sepVal vector.Any
	if len(args) == 2 {
		if sepVal = args[1]; sepVal.Type() != super.TypeString {
			return vector.NewWrappedError(j.sctx, "join: separator must be string", sepVal)
		}
	}
	out := vector.NewStringEmpty(0)
	inner := vector.Inner(splitsVal)
	for i := uint32(0); i < splitsVal.Len(); i++ {
		var seperator string
		if sepVal != nil {
			seperator = vector.StringValue(sepVal, i)
		}
		off, end := vector.ContainerOffset(splitsVal, i)
		var b strings.Builder
		var sep string
		for ; off < end; off++ {
			s := vector.StringValue(inner, off)
			b.WriteString(sep)
			b.WriteString(s)
			sep = seperator
		}
		out.Append(b.String())
	}
	return out
}

type Levenshtein struct {
	sctx *super.Context
}

func (l *Levenshtein) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(l.sctx, true, args, "levenshtein"); ok {
		return vec
	}
	args = underAll(args)
	for _, a := range args {
		if a.Type() != super.TypeString {
			return vector.NewWrappedError(l.sctx, "levenshtein: string args required", a)
		}
	}
	a, b := args[0], args[1]
	out := vector.NewIntEmpty(super.TypeInt64, a.Len())
	for i := uint32(0); i < a.Len(); i++ {
		as := vector.StringValue(a, i)
		bs := vector.StringValue(b, i)
		out.Append(int64(levenshtein.ComputeDistance(as, bs)))
	}
	return out
}

type Position struct {
	sctx *super.Context
}

func (p *Position) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(p.sctx, true, args, "position"); ok {
		return vec
	}
	args = underAll(args)
	vec, subVec := args[0], args[1]
	if vec.Type().ID() != super.IDString {
		return vector.NewWrappedError(p.sctx, "position: string arguments required", vec)
	}
	if subVec.Type().ID() != super.IDString {
		return vector.NewWrappedError(p.sctx, "position: string arguments required", subVec)
	}
	vals := make([]int64, vec.Len())
	for i := range vec.Len() {
		s := vector.StringValue(vec, i)
		sub := vector.StringValue(subVec, i)
		vals[i] = int64(strings.Index(s, sub) + 1)
	}
	return vector.NewInt(super.TypeInt64, vals)
}

type Replace struct {
	sctx *super.Context
}

func (r *Replace) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(r.sctx, true, args, "replace"); ok {
		return vec
	}
	args = underAll(args)
	for _, arg := range args {
		if arg.Type() != super.TypeString {
			return vector.NewWrappedError(r.sctx, "replace: string arg required", arg)
		}
	}
	sVal := args[0]
	out := vector.NewStringEmpty(0)
	for i := range sVal.Len() {
		s := vector.StringValue(sVal, i)
		old := vector.StringValue(args[1], i)
		new := vector.StringValue(args[2], i)
		out.Append(strings.ReplaceAll(s, old, new))
	}
	return out
}

type Split struct {
	sctx *super.Context
}

func (s *Split) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(s.sctx, true, args, "split"); ok {
		return vec
	}
	args = underAll(args)
	for i := range args {
		if args[i].Type() != super.TypeString {
			return vector.NewWrappedError(s.sctx, "split: string arg required", args[i])
		}
	}
	sVal, sepVal := args[0], args[1]
	var offsets []uint32
	values := vector.NewStringEmpty(0)
	var off uint32
	for i := uint32(0); i < sVal.Len(); i++ {
		ss := vector.StringValue(sVal, i)
		sep := vector.StringValue(sepVal, i)
		splits := strings.Split(ss, sep)
		for _, substr := range splits {
			values.Append(substr)
		}
		offsets = append(offsets, off)
		off += uint32(len(splits))
	}
	offsets = append(offsets, off)
	return vector.NewArray(s.sctx.LookupTypeArray(super.TypeString), offsets, values)
}

type ToLower struct {
	sctx *super.Context
}

func (t *ToLower) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(t.sctx, true, args, "lower"); ok {
		return vec
	}
	v := vector.Under(args[0])
	if v.Type() != super.TypeString {
		return vector.NewWrappedError(t.sctx, "lower: string arg required", v)
	}
	out := vector.NewStringEmpty(v.Len())
	for i := uint32(0); i < v.Len(); i++ {
		s := vector.StringValue(v, i)
		out.Append(strings.ToLower(s))
	}
	return out
}

type ToUpper struct {
	sctx *super.Context
}

func (t *ToUpper) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(t.sctx, true, args, "upper"); ok {
		return vec
	}
	v := vector.Under(args[0])
	if v.Type() != super.TypeString {
		return vector.NewWrappedError(t.sctx, "upper: string arg required", v)
	}
	out := vector.NewStringEmpty(v.Len())
	for i := uint32(0); i < v.Len(); i++ {
		s := vector.StringValue(v, i)
		out.Append(strings.ToUpper(s))
	}
	return out
}

type Trim struct {
	sctx *super.Context
}

func (t *Trim) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(t.sctx, true, args, "trim"); ok {
		return vec
	}
	val := vector.Under(args[0])
	if val.Type() != super.TypeString {
		return vector.NewWrappedError(t.sctx, "trim: string arg required", val)
	}
	out := vector.NewStringEmpty(val.Len())
	for i := uint32(0); i < val.Len(); i++ {
		s := vector.StringValue(val, i)
		out.Append(strings.TrimSpace(s))
	}
	return out
}
