package jsonvec

import (
	"encoding/binary"
	"slices"
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

func Materialize(sctx *super.Context, b Builder) vector.Any {
	m := &materializer{
		sctx: sctx,
		defs: super.NewTypeDefs(),
	}
	vec, _ := m.value(b.Value())
	return vec
}

type materializer struct {
	sctx   *super.Context
	defs   *super.TypeDefs
	mapper *super.TypeDefsMapper
}

func (m *materializer) value(v Value) (vector.Any, []uint32) {
	switch v := v.(type) {
	case *None:
		//missings are only used in empty arrays so length is always 0
		return vector.NewMissing(0), nil
	case *Null:
		return vector.NewNull(v.len), nil
	case *Bool:
		return vector.NewBool(v.Value), nil
	case *Int:
		return v.Value, nil
	case *Float:
		return v.Value, nil
	case *String:
		return v.Value, nil
	case *Union:
		return m.union(v)
	case *Array:
		return m.array(v)
	case *Record:
		return m.record(v)
	default:
		panic(v)
	}
}

func (m *materializer) union(u *Union) (vector.Any, []uint32) {
	var types []super.Type
	var vecs []vector.Any
	vals := u.Values()
	dynamic := make([][]uint32, 0, len(vals))
	fixed := make([]uint32, 0, len(vals))
	for _, v := range vals {
		vec, ids := m.value(v)
		dynamic = append(dynamic, ids)
		var id uint32
		if ids == nil {
			id = m.defs.LookupType(vec.Type())
		}
		fixed = append(fixed, uint32(id))
		vecs = append(vecs, vec)
		types = append(types, vec.Type())
	}
	if u.None != nil {
		types = append(types, super.TypeMissing)
		vecs = append(vecs, vector.NewEmpty(super.TypeMissing))
	}
	utyp := m.sctx.MustLookupTypeUnion(super.UniqueTypes(types))
	subtypes := m.makeUnionSubtypes(u.Tags, dynamic, fixed)
	typ := m.sctx.MustLookupTypeFusion(utyp)
	loader := &subtypesLoader{
		defs:     m.defs,
		subtypes: subtypes,
	}
	vec := vector.NewUnion(utyp, u.Tags, vecs)
	return vector.NewFusionWithLoader(m.sctx, typ, loader, vec), subtypes
}

func (m *materializer) makeUnionSubtypes(tags []uint32, dynamic [][]uint32, fixed []uint32) []uint32 {
	subtypes := make([]uint32, 0, len(tags))
	for _, tag := range tags {
		var id uint32
		if dynamic[tag] != nil {
			id = dynamic[tag][0]
			dynamic[tag] = dynamic[tag][1:]
		} else {
			id = fixed[tag]
		}
		subtypes = append(subtypes, id)
	}
	return subtypes
}

func (m *materializer) array(a *Array) (vector.Any, []uint32) {
	inner, ids := m.value(a.Inner)
	arrayType := m.sctx.LookupTypeArray(inner.Type())
	array := vector.NewArray(arrayType, a.Offsets, inner)
	if ids == nil {
		// There is only one type.
		return array, nil
	}
	n := len(a.Offsets) - 1
	subtypes := make([]uint32, 0, n)
	for k := range n {
		id := m.arrayID(ids[a.Offsets[k]:a.Offsets[k+1]])
		subtypes = append(subtypes, id)
	}
	fusionType := m.sctx.MustLookupTypeFusion(arrayType)
	loader := &subtypesLoader{
		defs:     m.defs,
		subtypes: subtypes,
	}
	return vector.NewFusionWithLoader(m.sctx, fusionType, loader, array), subtypes
}

func (m *materializer) arrayID(ids []uint32) uint32 {
	if len(ids) == 0 {
		return m.defs.BindTypeWrapped(super.TypeDefArray, super.IDMissing)
	}
	id := ids[0]
	for _, other := range ids[1:] {
		if other != id {
			return m.unionArrayID(ids)
		}
	}
	return m.defs.BindTypeWrapped(super.TypeDefArray, id)
}

func (m *materializer) unionArrayID(ids []uint32) uint32 {
	// If this heavyweight lookup becomes a bottleneck, we should optimize.
	if m.mapper == nil {
		m.mapper = super.NewTypeDefsMapper(super.NewContext(), m.defs)
	}
	uniq := make(map[super.Type]struct{})
	for _, id := range ids {
		uniq[m.mapper.LookupType(id)] = struct{}{}
	}
	types := make([]super.Type, 0, len(uniq))
	for typ := range uniq {
		types = append(types, typ)
	}
	unionID := m.mapper.LookupTypeUnion(types)
	return m.defs.BindTypeWrapped(super.TypeDefArray, unionID)
}

func (m *materializer) record(r *Record) (vector.Any, []uint32) {
	fuseHere := !permsOrdered(r.perm)
	needIDs := fuseHere
	fieldNames := make([]string, len(r.LUT))
	for name, id := range r.LUT {
		fieldNames[id] = name
	}
	n := r.Len()
	var vecs []vector.Any
	var allFields []super.Field
	// dynamic and fixed are indexed by the supertype's column number.
	// dynamic holds the subtype ID vectors of fields that have them
	// attached (because there is a fusion type below).
	// Otherwise, fixed holds the constant ID for all all rows in that
	// field in each column position.
	dynamic := make([][]uint32, len(r.Fields))
	fixed := make([]uint32, len(r.Fields))
	for i, field := range r.Fields {
		rle := r.RLEs[i].End(n)
		vec, ids := m.value(field.Value)
		dynamic[i] = ids
		// XXX We shouldn't always pollute the typedefs with this
		// type... only if there is a demand for fusion, i.e.,
		// don't put the type ID in the typedefs if it's going to
		// be thrown away?!  This isn't all that big a deal
		// and we can cleanup later.
		if ids == nil {
			fixed[i] = m.defs.LookupType(vec.Type())
		} else {
			needIDs = true
		}
		if len(rle) > 0 {
			needIDs = true
			vec = m.missing(vec, ids, fixed[i], n, rle)
		}
		vecs = append(vecs, vec)
		allFields = append(allFields, super.NewField(fieldNames[i], vec.Type()))
	}
	rtyp := m.sctx.MustLookupTypeRecord(allFields)
	record := vector.NewRecord(rtyp, vecs, n)
	if !needIDs {
		return record, nil
	}
	subtypes := m.makeRecordSubtypes(r.perm, allFields, dynamic, fixed, r.tags)
	if !fuseHere {
		// Field order is consistent and varying fields carry their own
		// fusions, so the record itself isn't fused. Enclosing fusions
		// still need the per-row original types for their subtypes.
		return record, subtypes
	}
	typ := m.sctx.MustLookupTypeFusion(rtyp)
	loader := &subtypesLoader{
		defs:     m.defs,
		subtypes: subtypes,
	}
	return vector.NewFusionWithLoader(m.sctx, typ, loader, record), subtypes
}

// missing wraps a field that is absent in some rows (per rle) as
// fusion((T|none)): a plain union with none, not option(T). Its subtypes
// are none where the field is absent, so defuse removes the field there,
// and otherwise the field's own subtype. The field doesn't depend on the
// parent record's subtypes.
func (m *materializer) missing(vec vector.Any, ids []uint32, fixed uint32, n uint32, rle []uint32) vector.Any {
	if f, ok := vec.(*vector.Fusion); ok {
		// Flatten to a single fusion((T1|...|none)) like the fuser does.
		// ids already carry the child's per-value subtypes.
		vec = f.Values
	}
	// This builds a *vector.Union of type (T|none) via sctx.UnionWithNone
	// If vec is itself a union, its tags are kept and none is appended.
	u := vector.NewUnionNoneRLE(m.sctx, vec, n, rle)
	// Tags index the union's vectors, not Typ.Types, so find none there.
	missingTag := uint32(slices.IndexFunc(u.Values(), func(v vector.Any) bool {
		return v.Type() == super.TypeMissing
	}))
	subtypes := make([]uint32, 0, n)
	for _, tag := range u.Tags() {
		switch {
		case tag == missingTag:
			subtypes = append(subtypes, super.IDMissing)
		case ids != nil:
			subtypes = append(subtypes, ids[0])
			ids = ids[1:]
		default:
			subtypes = append(subtypes, fixed)
		}
	}
	typ := m.sctx.MustLookupTypeFusion(u.Typ)
	loader := &subtypesLoader{defs: m.defs, subtypes: subtypes}
	return vector.NewFusionWithLoader(m.sctx, typ, loader, u)
}

// permsOrdered reports whether every key permutation lists its columns in
// increasing order, i.e., each row's fields appear in the same relative
// order as the fused record, so no record-level fusion is needed.
func permsOrdered(perm map[string]uint32) bool {
	for desc := range perm {
		r := strings.NewReader(desc)
		prev := -1
		for {
			col, err := binary.ReadUvarint(r)
			if err != nil {
				break
			}
			if int(col) <= prev {
				return false
			}
			prev = int(col)
		}
	}
	return true
}

func (m *materializer) makeRecordSubtypes(perm map[string]uint32, fields []super.Field, dynamic [][]uint32, fixed []uint32, tags []uint32) []uint32 {
	// We make a template for each permutation, which is a list of
	// type IDs corresponding to the types of each field.  These
	// will get overwrriten on each lookup to the actual type based
	// on any children subtypes or fixed type ID.  There is also
	// a names slice for each pemutation.
	templates := make([][]int32, len(perm))
	names := make([][]string, len(perm))
	for desc, tag := range perm {
		r := strings.NewReader(desc)
		for {
			col, err := binary.ReadUvarint(r)
			if err != nil {
				break
			}
			templates[tag] = append(templates[tag], int32(col))
			names[tag] = append(names[tag], fields[col].Name)
		}
	}
	// XXX There are a couple optimizations we could do here.
	// First, we could serialize the scatch lookup as the actual
	// typedefs table key directly instead of serializing scratch IDs
	// then making the typedefs key in BindTypeRecord. Second, we
	// could limit the lookups to just each permutation of the
	// record types that is equal to the cardinality of the subtypes
	// slice output.  We will wait on these ideas until profiling
	// suggests otherwise.
	var scratch []uint32
	subtypes := make([]uint32, len(tags))
	for i, tag := range tags {
		scratch = scratch[:0]
		for _, col := range templates[tag] {
			if dynamic[col] != nil {
				scratch = append(scratch, dynamic[col][0])
				dynamic[col] = dynamic[col][1:]
			} else {
				scratch = append(scratch, fixed[col])
			}
		}
		subtypes[i] = m.defs.BindTypeRecord(names[tag], scratch, nil)
	}
	return subtypes
}

type subtypesLoader struct {
	defs     *super.TypeDefs
	subtypes []uint32
}

func (s *subtypesLoader) Load() (*super.TypeDefs, []uint32) {
	return s.defs, s.subtypes
}
