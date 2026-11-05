package cast

import (
	"github.com/superdb/super"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
)

func castToType(sctx *super.Context, vec vector.Any, index []uint32) (vector.Any, []uint32, string, bool) {
	switch vec := vec.(type) {
	case *vector.TypeValue:
		return vec, nil, "", true
	case *vector.String:
		n := lengthOf(vec, index)
		out := vector.NewTypeValueEmpty(sctx)
		var errs []uint32
		for i := range n {
			idx := i
			if index != nil {
				idx = index[i]
			}
			val, err := sup.ParseValue(sctx, vec.Value(idx))
			if err != nil || val.Type().ID() != super.IDType {
				errs = append(errs, i)
				continue
			}
			typ, tv := sctx.DecodeTypeValue(val.Bytes())
			if tv == nil {
				errs = append(errs, i)
				continue
			}
			out.Append(typ)
		}
		return out, errs, "", true
	default:
		return nil, nil, "", false
	}
}
