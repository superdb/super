package function

import (
	"github.com/lestrrat-go/strftime"
	"github.com/superdb/super"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/expr/cast"
	"github.com/superdb/super/vector"
)

type Bucket struct {
	name string
	sctx *super.Context
}

func (b *Bucket) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(b.sctx, true, args, "bucket"); ok {
		return vec
	}
	args = underAll(args)
	tsArg, binArg := args[0], args[1]
	tsID, binID := tsArg.Type().ID(), binArg.Type().ID()
	if tsID != super.IDDuration && tsID != super.IDTime {
		return vector.NewWrappedError(b.sctx, b.name+": first argument is not a time or duration", tsArg)
	}
	if binID != super.IDDuration {
		return vector.NewWrappedError(b.sctx, b.name+": second argument is not a duration", binArg)
	}
	return vector.Apply(vector.ApplyNone, b.call, tsArg, binArg)
}

func (b *Bucket) call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(b.sctx, true, args, "bucket"); ok {
		return vec
	}
	tsArg, binArg := args[0], args[1]
	if _, ok := binArg.(*vector.Const); ok {
		bin := vector.IntValue(binArg, 0)
		return b.constBin(tsArg, nano.Duration(bin))
	}
	var ints []int64
	for i := range tsArg.Len() {
		dur := vector.IntValue(tsArg, i)
		bin := vector.IntValue(binArg, i)
		if bin == 0 {
			ints = append(ints, dur)
		} else {
			ints = append(ints, int64(nano.Ts(dur).Trunc(nano.Duration(bin))))
		}
	}
	return vector.NewInt(b.resultType(tsArg), ints)
}

func (b *Bucket) constBin(tsVec vector.Any, bin nano.Duration) vector.Any {
	if bin == 0 {
		return cast.To(b.sctx, tsVec, b.resultType(tsVec))
	}
	switch tsVec := tsVec.(type) {
	case *vector.Const:
		typ := b.resultType(tsVec)
		ts := vector.IntValue(tsVec, 0)
		return vector.NewConstInt(typ, ts, tsVec.Len())
	case *vector.View:
		return vector.NewView(b.constBinFlat(tsVec.Any, bin), tsVec.Index)
	case *vector.Dict:
		return vector.NewDict(b.constBinFlat(tsVec.Any, bin), tsVec.Index, tsVec.Counts)
	default:
		return b.constBinFlat(tsVec, bin)
	}
}

func (b *Bucket) constBinFlat(tsVecFlat vector.Any, bin nano.Duration) *vector.Int {
	tsVec := tsVecFlat.(*vector.Int)
	ints := make([]int64, tsVec.Len())
	for i := range tsVec.Len() {
		if bin == 0 {
			ints[i] = tsVec.Values[i]
		} else {
			ints[i] = int64(nano.Ts(tsVec.Values[i]).Trunc(bin))
		}
	}
	return vector.NewInt(b.resultType(tsVec), ints)
}

func (b *Bucket) resultType(tsVec vector.Any) super.Type {
	if tsVec.Type().ID() == super.IDDuration {
		return super.TypeDuration
	}
	return super.TypeTime
}

type Now struct{}

func (*Now) needsInput() {}

func (n *Now) Call(args ...vector.Any) vector.Any {
	v := int64(nano.Now())
	return vector.NewConstInt(super.TypeTime, v, args[0].Len())
}

type Strftime struct {
	sctx *super.Context
}

func (s *Strftime) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.Catch(s.sctx, true, args, "strftime"); ok {
		return vec
	}
	args = underAll(args)
	formatVec, timeVec := args[0], args[1]
	if formatVec.Type().ID() != super.IDString {
		return vector.NewWrappedError(s.sctx, "strftime: string value required for format arg", formatVec)
	}
	if timeVec.Type().ID() != super.IDTime {
		return vector.NewWrappedError(s.sctx, "strftime: time value required for time arg", args[1])
	}
	if _, ok := formatVec.(*vector.Const); ok {
		return s.fastPath(formatVec, timeVec)
	}
	return s.slowPath(formatVec, timeVec)
}

func (s *Strftime) fastPath(fvec vector.Any, tvec vector.Any) vector.Any {
	format := vector.StringValue(fvec, 0)
	f, err := strftime.New(format)
	if err != nil {
		return vector.NewWrappedError(s.sctx, "strftime: "+err.Error(), fvec)
	}
	switch tvec := tvec.(type) {
	case *vector.Int:
		return s.fastPathLoop(f, tvec, nil)
	case *vector.Const:
		t := vector.IntValue(tvec, 0)
		s := f.FormatString(nano.Ts(t).Time())
		return vector.NewConstString(s, tvec.Len())
	case *vector.View:
		return s.fastPathLoop(f, tvec.Any.(*vector.Int), tvec.Index)
	case *vector.Dict:
		vec := s.fastPathLoop(f, tvec.Any.(*vector.Int), nil)
		return vector.NewDict(vec, tvec.Index, tvec.Counts)
	default:
		panic(tvec)
	}
}

func (s *Strftime) fastPathLoop(f *strftime.Strftime, vec *vector.Int, index []uint32) *vector.String {
	if index != nil {
		out := vector.NewStringEmpty(uint32(len(index)))
		for _, i := range index {
			s := f.FormatString(nano.Ts(vec.Values[i]).Time())
			out.Append(s)
		}
		return out
	}
	out := vector.NewStringEmpty(vec.Len())
	for i := range vec.Len() {
		s := f.FormatString(nano.Ts(vec.Values[i]).Time())
		out.Append(s)
	}
	return out
}

func (s *Strftime) slowPath(fvec vector.Any, tvec vector.Any) vector.Any {
	var f *strftime.Strftime
	var errIndex []uint32
	errMsgs := vector.NewStringEmpty(0)
	out := vector.NewStringEmpty(0)
	for i := range fvec.Len() {
		format := vector.StringValue(fvec, i)
		if f == nil || f.Pattern() != format {
			var err error
			f, err = strftime.New(format)
			if err != nil {
				errIndex = append(errIndex, i)
				errMsgs.Append("strftime: " + err.Error())
				continue
			}
		}
		t := vector.IntValue(tvec, i)
		out.Append(f.FormatString(nano.Ts(t).Time()))
	}
	if len(errIndex) > 0 {
		errVec := vector.NewVecWrappedError(s.sctx, errMsgs, vector.Pick(fvec, errIndex))
		return vector.Combine(out, errIndex, errVec)
	}
	return out
}
