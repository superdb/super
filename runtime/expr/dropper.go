package expr

import (
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

// Dropper drops one or more fields in a record.  If it drops all fields of a
// top-level record, the record is replaced by an empty record.  If it drops all
// fields of a nested record, the nested record is dropped.  Dropper does not
// modify non-records.  A union (named or not) is treated as the value of its
// member so if Dropper modifies any member, the union is replaced by the values
// of its members (modified or not), as for the type computed by the semantic
// checker.
type Dropper struct {
	sctx   *super.Context
	defuse *SuperDefuse
	fm     fieldsMap
}

func NewDropper(sctx *super.Context, fields field.List) *Dropper {
	fm := fieldsMap{}
	for _, f := range fields {
		fm.Add(f)
	}
	return &Dropper{sctx, NewSuperDefuse(sctx), fm}
}

func (d *Dropper) Eval(vec vector.Any) vector.Any {
	return vector.Apply(vector.ApplyNone, d.eval, d.defuse.Eval(vec))
}

func (d *Dropper) eval(vecs ...vector.Any) vector.Any {
	vec := vecs[0]
	if k := vec.Type().Kind(); k != super.RecordKind && k != super.UnionKind {
		return vec
	}
	if vec2, ok := d.drop(vec, d.fm); ok {
		if dynamic, ok := vec2.(*vector.Dynamic); ok {
			values := slices.Clone(dynamic.Values)
			for k, rev := range reverseTags(dynamic) {
				if values[k] == nil {
					values[k] = d.emptyRecord(uint32(len(rev)))
				}
			}
			return vector.NewDynamic(dynamic.Tags, values)
		}
		if vec2 == nil {
			// Dropped all fields.
			return d.emptyRecord(vec.Len())
		}
		return vec2
	}
	return vec
}

func (d *Dropper) emptyRecord(n uint32) vector.Any {
	return vector.NewRecord(d.sctx.MustLookupTypeRecord(nil), nil, n)
}

// drop drops the fields in fm from vec.  It returns nil, false if vec is not a
// record or a union or no fields were dropped; nil, true if all fields were
// dropped; and non-nil, true if some fields were dropped or modified.  For a
// union, the non-nil vector is a Dynamic whose values are the results for the
// members, where a nil value means all fields were dropped.
func (d *Dropper) drop(val vector.Any, fm fieldsMap) (vector.Any, bool) {
	switch val := vector.Under(val).(type) {
	case *vector.Union:
		dynamic := val.Dynamic()
		values := make([]vector.Any, len(dynamic.Values))
		var changed bool
		for k, index := range reverseTags(dynamic) {
			if len(index) == 0 {
				continue
			}
			values[k] = dynamic.Values[k]
			if vec, ok := d.drop(values[k], fm); ok {
				values[k] = vec
				changed = true
			}
		}
		if !changed {
			return nil, false
		}
		return newDynamic(dynamic.Tags, values), true
	case *vector.Record:
		rec := val
		var vecFields []vector.Any
		var typFields []super.Field
		var changed bool
		for i, f := range super.TypeRecordOf(val.Type()).Fields {
			valField := val.Fields[i]
			if ff, ok := fm[f.Name]; ok {
				if ff == nil {
					// Drop field.
					changed = true
					continue
				}
				if val, ok := d.drop(valField, ff); ok {
					if dynamic, ok := val.(*vector.Dynamic); ok {
						// The field is a union whose members
						// were modified differently.
						return d.ripRecord(rec, i, dynamic, fm), true
					}
					changed = true
					if val == nil {
						// Drop field since we dropped all its subfields.
						continue
					}
					// Substitute modified field.
					vecFields = append(vecFields, val)
					typFields = append(typFields, super.NewField(f.Name, val.Type()))
					continue
				}
			}
			// Keep field.
			vecFields = append(vecFields, valField)
			typFields = append(typFields, super.NewField(f.Name, valField.Type()))
		}
		if !changed {
			return nil, false
		}
		if len(vecFields) == 0 {
			return nil, true
		}
		typ := d.sctx.MustLookupTypeRecord(typFields)
		return vector.NewRecord(typ, vecFields, val.Len()), true
	case *vector.View:
		if vec := vector.PushView(val); vec != vector.Any(val) {
			return d.drop(vec, fm)
		}
	}
	return val, false
}

// ripRecord returns a Dynamic for the results of dropping the fields in fm
// from rec when field i of rec has been dropped from with a result of
// dynamic: for each value of dynamic, the result for the record with the
// value of dynamic for field i (or without field i if the value is nil).
// Since dropping is idempotent, the fields in fm are dropped from each record
// again to drop the other fields.
func (d *Dropper) ripRecord(rec *vector.Record, i int, dynamic *vector.Dynamic, fm fieldsMap) vector.Any {
	typ := super.TypeRecordOf(rec.Type())
	values := make([]vector.Any, len(dynamic.Values))
	for k, index := range reverseTags(dynamic) {
		if len(index) == 0 {
			continue
		}
		var vecFields []vector.Any
		var typFields []super.Field
		for j, f := range typ.Fields {
			var vec vector.Any
			if j == i {
				if vec = dynamic.Values[k]; vec == nil {
					continue
				}
			} else {
				vec = vector.Pick(rec.Fields[j], index)
			}
			vecFields = append(vecFields, vec)
			typFields = append(typFields, super.NewField(f.Name, vec.Type()))
		}
		if len(vecFields) == 0 {
			continue
		}
		vec := vector.Any(vector.NewRecord(d.sctx.MustLookupTypeRecord(typFields), vecFields, uint32(len(index))))
		if vec2, ok := d.drop(vec, fm); ok {
			vec = vec2
		}
		values[k] = vec
	}
	return newDynamic(dynamic.Tags, values)
}

// reverseTags returns the slots of dynamic for each of its values.  Unlike
// Dynamic.ReverseTagMap, it allows nil values.
func reverseTags(dynamic *vector.Dynamic) [][]uint32 {
	reverse := make([][]uint32, len(dynamic.Values))
	for slot, tag := range dynamic.Tags {
		reverse[tag] = append(reverse[tag], uint32(slot))
	}
	return reverse
}

// newDynamic returns a Dynamic for tags and values where the values that are
// Dynamics are flattened into it.  Unlike vector.Stitch, it keeps nil values.
func newDynamic(tags []uint32, values []vector.Any) *vector.Dynamic {
	isDynamic := func(vec vector.Any) bool {
		_, ok := vec.(*vector.Dynamic)
		return ok
	}
	if !slices.ContainsFunc(values, isDynamic) {
		return vector.NewDynamic(tags, values)
	}
	var out []vector.Any
	base := make([]uint32, len(values))
	nested := make([][]uint32, len(values))
	for k, vec := range values {
		base[k] = uint32(len(out))
		if dynamic, ok := vec.(*vector.Dynamic); ok {
			out = append(out, dynamic.Values...)
			nested[k] = dynamic.Tags
		} else {
			out = append(out, vec)
		}
	}
	newTags := make([]uint32, len(tags))
	for i, tag := range tags {
		newTags[i] = base[tag]
		if n := nested[tag]; n != nil {
			newTags[i] += n[0]
			nested[tag] = n[1:]
		}
	}
	return vector.NewDynamic(newTags, out)
}

type fieldsMap map[string]fieldsMap

func (f fieldsMap) Add(path field.Path) {
	if len(path) == 1 {
		f[path[0]] = nil
	} else if len(path) > 1 {
		ff, ok := f[path[0]]
		if ff == nil {
			if ok {
				return
			}
			ff = fieldsMap{}
			f[path[0]] = ff
		}
		ff.Add(path[1:])
	}
}
