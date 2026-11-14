package expr

import (
	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

// ripUnions returns vec with each union (named or not) that is vec or that is
// on the path to a field in paths (but not the field itself) replaced by the
// values of its members.  If there are such unions, it returns a Dynamic
// whose values are the values of vec split by member (with nil values for
// the members without values), and otherwise vec.
func ripUnions(sctx *super.Context, vec vector.Any, paths []field.Path) vector.Any {
	switch under := vector.Under(vec).(type) {
	case *vector.Union:
		dynamic := under.Dynamic()
		values := make([]vector.Any, len(dynamic.Values))
		for k, val := range dynamic.Values {
			values[k] = ripUnions(sctx, val, paths)
		}
		return newDynamic(dynamic.Tags, values)
	case *vector.Record, *vector.View:
		rec, ok := vector.PushView(under).(*vector.Record)
		if !ok {
			return vec
		}
		typ := super.TypeRecordOf(rec.Type())
		for _, path := range paths {
			if len(path) < 2 {
				continue
			}
			i, ok := typ.IndexOfField(path[0])
			if !ok {
				continue
			}
			dynamic, ok := ripUnions(sctx, rec.Fields[i], tails(paths, path[0])).(*vector.Dynamic)
			if !ok {
				continue
			}
			// Split rec by the values of field i and rip each part
			// for the other fields.
			named, _ := vec.Type().(*super.TypeNamed)
			values := make([]vector.Any, len(dynamic.Values))
			for k, index := range reverseTags(dynamic) {
				if len(index) == 0 {
					continue
				}
				fields := make([]vector.Any, len(rec.Fields))
				typFields := make([]super.Field, len(rec.Fields))
				for j, f := range typ.Fields {
					if j == i {
						fields[j] = dynamic.Values[k]
					} else {
						fields[j] = vector.Pick(rec.Fields[j], index)
					}
					typFields[j] = super.NewField(f.Name, fields[j].Type())
				}
				var part vector.Any = vector.NewRecord(sctx.MustLookupTypeRecord(typFields), fields, uint32(len(index)))
				if named != nil && part.Type() == named.Type {
					part = vector.NewNamed(named, part)
				}
				values[k] = ripUnions(sctx, part, paths)
			}
			return newDynamic(dynamic.Tags, values)
		}
	}
	return vec
}

// tails returns the paths in paths that start with name without name.
func tails(paths []field.Path, name string) []field.Path {
	var out []field.Path
	for _, path := range paths {
		if len(path) > 1 && path[0] == name {
			out = append(out, path[1:])
		}
	}
	return out
}
