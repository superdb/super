package function

import (
	"github.com/superdb/super"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

// paths returns the path collection of a value, i.e., the paths of the
// leaves of the value as produced by flatten.  For a type value, it returns the
// paths of the fields of a record type, descending into the fields that are
// records.
type paths struct {
	sctx *super.Context
	f    *flatten
	b    scode.Builder
}

func newPaths(sctx *super.Context) *paths {
	return &paths{sctx: sctx, f: newFlatten(sctx)}
}

func (p *paths) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.CatchFirst(p.sctx, expr.CatchAll, args, "paths"); ok {
		return vec
	}
	vec := args[0]
	out := vector.NewDynamicValueBuilder()
	for i := range vec.Len() {
		out.Write(p.paths(vector.ValueAt(&p.b, vec, i)))
	}
	return out.Build(p.sctx)
}

func (p *paths) paths(val super.Value) super.Value {
	val = untag(val)
	if super.TypeUnder(val.Type()) == super.TypeType {
		typ, err := p.sctx.LookupByValue(val.Bytes())
		if err != nil {
			panic(err)
		}
		rec, ok := super.TypeUnder(typ).(*super.TypeRecord)
		if !ok {
			return p.sctx.WrapError("paths: not a record type", val)
		}
		var paths []super.Value
		for _, path := range recordTypePaths(rec, nil) {
			paths = append(paths, p.path(path))
		}
		return buildArray(p.sctx, paths)
	}
	if !isContainer(val) {
		// A primitive value is a leaf at the root.
		return buildArray(p.sctx, []super.Value{p.path(nil)})
	}
	p.f.pvs = p.f.pvs[:0]
	p.f.walk(nil, val)
	var paths []super.Value
	for _, pv := range p.f.pvs {
		paths = append(paths, p.path(pv.path))
	}
	return buildArray(p.sctx, paths)
}

// path returns the path value of elems.  An empty path is of type [string].
func (p *paths) path(elems []super.Value) super.Value {
	if len(elems) == 0 {
		return super.NewValue(p.sctx.LookupTypeArray(super.TypeString), scode.Bytes{})
	}
	return buildArray(p.sctx, elems)
}

// recordTypePaths returns the paths of the fields of typ, descending into the
// fields whose type is a record type.
func recordTypePaths(typ *super.TypeRecord, prefix []super.Value) [][]super.Value {
	var out [][]super.Value
	for _, f := range typ.Fields {
		path := extend(prefix, super.NewString(f.Name))
		if rec, ok := super.TypeUnder(f.Type).(*super.TypeRecord); ok && len(rec.Fields) > 0 {
			out = append(out, recordTypePaths(rec, path)...)
		} else {
			out = append(out, path)
		}
	}
	return out
}

// keys returns the field names of a record or a record type value in order and
// the keys of a map.
type keys struct {
	sctx *super.Context
}

func newKeys(sctx *super.Context) *keys {
	return &keys{sctx: sctx}
}

func (k *keys) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.CatchFirst(k.sctx, expr.CatchAll, args, "keys"); ok {
		return vec
	}
	vec := vector.Under(args[0])
	switch typ := super.TypeUnder(vec.Type()).(type) {
	case *super.TypeRecord:
		return k.recordKeys([]*super.TypeRecord{typ}, make([]uint32, vec.Len()))
	case *super.TypeMap:
		m := vector.PushView(vec).(*vector.Map)
		return vector.NewArray(k.sctx.LookupTypeArray(typ.KeyType), m.Offsets, m.Keys)
	case *super.TypeOfType:
		var recs []*super.TypeRecord
		var index, errs []uint32
		which := make(map[*super.TypeRecord]uint32)
		for i := range vec.Len() {
			typ := super.TypeUnder(vector.TypeValueValue(vec, i))
			rec, ok := typ.(*super.TypeRecord)
			if !ok {
				errs = append(errs, i)
				continue
			}
			tag, ok := which[rec]
			if !ok {
				tag = uint32(len(recs))
				which[rec] = tag
				recs = append(recs, rec)
			}
			index = append(index, tag)
		}
		out := k.recordKeys(recs, index)
		if len(errs) > 0 {
			return vector.Combine(out, errs, vector.NewWrappedError(k.sctx, "keys: not a record type", vector.Pick(vec, errs)))
		}
		return out
	}
	return vector.NewWrappedError(k.sctx, "keys: not a record or map", vec)
}

// recordKeys returns an array of the field names of recs[index[i]] for each i.
func (k *keys) recordKeys(recs []*super.TypeRecord, index []uint32) vector.Any {
	s := vector.NewStringEmpty(0)
	offsets := []uint32{0}
	for _, tag := range index {
		for _, f := range recs[tag].Fields {
			s.Append(f.Name)
		}
		offsets = append(offsets, offsets[len(offsets)-1]+uint32(len(recs[tag].Fields)))
	}
	return vector.NewArray(k.sctx.LookupTypeArray(super.TypeString), offsets, s)
}

// values returns the field values of a record in order and the values of a
// map.
type values struct {
	sctx *super.Context
	b    scode.Builder
}

func newValues(sctx *super.Context) *values {
	return &values{sctx: sctx}
}

func (v *values) Call(args ...vector.Any) vector.Any {
	if vec, ok := expr.CatchFirst(v.sctx, expr.CatchAll, args, "values"); ok {
		return vec
	}
	vec := vector.Under(args[0])
	switch typ := super.TypeUnder(vec.Type()).(type) {
	case *super.TypeRecord:
		out := vector.NewDynamicValueBuilder()
		for i := range vec.Len() {
			val := vector.ValueAt(&v.b, vec, i)
			var vals []super.Value
			it := val.Bytes().Iter()
			for _, f := range typ.Fields {
				fval := super.NewValue(f.Type, it.Next())
				if fval.IsNone() {
					// An absent optional field has no value.
					continue
				}
				vals = append(vals, untag(fval).Copy())
			}
			if len(vals) == 0 {
				out.Write(super.NewValue(v.sctx.LookupTypeArray(super.TypeNull), scode.Bytes{}))
				continue
			}
			out.Write(buildArray(v.sctx, vals))
		}
		return out.Build(v.sctx)
	case *super.TypeMap:
		m := vector.PushView(vec).(*vector.Map)
		return vector.NewArray(v.sctx.LookupTypeArray(typ.ValType), m.Offsets, m.Values)
	}
	return vector.NewWrappedError(v.sctx, "values: not a record or map", vec)
}
