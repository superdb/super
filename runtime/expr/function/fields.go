package function

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/vector"
)

type Fields struct {
	sctx     *super.Context
	innerTyp *super.TypeArray
	outerTyp *super.TypeArray
}

func NewFields(sctx *super.Context) *Fields {
	inner := sctx.LookupTypeArray(super.TypeString)
	return &Fields{
		sctx:     sctx,
		innerTyp: inner,
		outerTyp: sctx.LookupTypeArray(inner),
	}
}

func (f *Fields) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.CheckForErrorThenNullThenNone(f.sctx, args, "fields"); ok {
		return vec
	}
	val := vector.Under(args[0])
	switch typ := val.Type().(type) {
	case *super.TypeRecord:
		paths := buildPath(typ, nil)
		s := vector.NewStringEmpty(val.Len())
		inOffs, outOffs := []uint32{0}, []uint32{0}
		for i := uint32(0); i < val.Len(); i++ {
			inOffs, outOffs = appendPaths(paths, s, inOffs, outOffs)
		}
		inner := vector.NewArray(f.innerTyp, inOffs, s)
		return vector.NewArray(f.outerTyp, outOffs, inner)
	case *super.TypeOfType:
		var errs []uint32
		s := vector.NewStringEmpty(val.Len())
		inOffs, outOffs := []uint32{0}, []uint32{0}
		for i := uint32(0); i < val.Len(); i++ {
			rtyp, ok := vector.TypeValueValue(val, i).(*super.TypeRecord)
			if !ok {
				errs = append(errs, i)
				continue
			}
			inOffs, outOffs = appendPaths(buildPath(rtyp, nil), s, inOffs, outOffs)
		}
		inner := vector.NewArray(f.innerTyp, inOffs, s)
		out := vector.NewArray(f.outerTyp, outOffs, inner)
		if len(errs) > 0 {
			return vector.Combine(out, errs, vector.NewStringError(f.sctx, "missing", uint32(len(errs))))
		}
		return out
	default:
		return vector.NewStringError(f.sctx, "missing", val.Len())
	}
}

func buildPath(typ *super.TypeRecord, prefix []string) [][]string {
	var out [][]string
	for _, f := range typ.Fields {
		if typ, ok := super.TypeUnder(f.Type).(*super.TypeRecord); ok {
			out = append(out, buildPath(typ, append(prefix, f.Name))...)
		} else {
			out = append(out, append(prefix, f.Name))
		}
	}
	return out
}

func appendPaths(paths [][]string, s *vector.String, inner, outer []uint32) ([]uint32, []uint32) {
	for _, path := range paths {
		for _, f := range path {
			s.Append(f)
		}
		inner = append(inner, inner[len(inner)-1]+uint32(len(path)))
	}
	return inner, append(outer, outer[len(outer)-1]+uint32(len(paths)))
}
