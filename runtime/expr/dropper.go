package expr

import (
	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

// Dropper drops one or more fields in a record.  If it drops all fields of a
// top-level record, the record is replaced by an empty record.  If it drops all
// fields of a nested record, the nested record is dropped.  Dropper does not
// modify non-records.
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
	if vec.Type().Kind() != super.RecordKind {
		return vec
	}
	if vec2, ok := d.drop(vec, d.fm); ok {
		if vec2 == nil {
			// Dropped all fields.
			return vector.NewRecord(d.sctx.MustLookupTypeRecord(nil), nil, vec.Len())
		}
		return vec2
	}
	return vec
}

// drop drops the fields in fm from vec.  It returns nil, false if vec is not a
// record or no fields were dropped; nil, true if all fields were dropped; and
// non-nil, true if some fields were dropped or modified.
func (d *Dropper) drop(val vector.Any, fm fieldsMap) (vector.Any, bool) {
	switch val := vector.Under(val).(type) {
	case *vector.Record:
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
	case *vector.Dict:
		if newVec, ok := d.drop(val.Any, fm); ok {
			return vector.NewDict(newVec, val.Index, val.Counts), true
		}
	case *vector.View:
		if newVec, ok := d.drop(val.Any, fm); ok {
			return vector.Pick(newVec, val.Index), true
		}
	}
	return val, false
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
