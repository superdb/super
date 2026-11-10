package cast

import (
	"slices"
	"strconv"
	"strings"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vbuild"
)

// Cast casts vec to typ, which may be any type.  It is the vector
// implementation of the sam caster (runtime/sam/expr/function/cast.go) and
// has the same semantics.  Like the sam caster, it expects vec to have been
// defused.
func Cast(sctx *super.Context, vec vector.Any, typ super.Type) vector.Any {
	return CastWithOptions(sctx, vec, typ, Options{})
}

// Options alter the semantics of Cast for loading values into database
// tables.
type Options struct {
	// AbsentToNull causes a record field absent from a value to be null
	// rather than an error when the field's type admits null (i.e., is
	// null or a union with null), as with the columns of a SQL table.
	AbsentToNull bool
	// Strict causes a record with a field not in the target record type
	// to be an error rather than the field being dropped.
	Strict bool
}

// CastWithOptions is like Cast but with the semantics altered by opts.
func CastWithOptions(sctx *super.Context, vec vector.Any, typ super.Type, opts Options) vector.Any {
	c := &complexCaster{sctx: sctx, opts: opts}
	return c.cast(vec, typ)
}

type complexCaster struct {
	sctx *super.Context
	opts Options
}

// cast casts vec, which may be a union or a dynamic, by casting each of its
// values of a single type.
func (c *complexCaster) cast(vec vector.Any, to super.Type) vector.Any {
	return vector.Apply(vector.ApplyRipUnions|vector.ApplyRipOptions, func(vecs ...vector.Any) vector.Any {
		return c.castOne(vecs[0], to)
	}, vec)
}

// castOne casts vec, whose values are of a single type, to to.
func (c *complexCaster) castOne(vec vector.Any, to super.Type) vector.Any {
	n := vec.Len()
	if n == 0 {
		return vector.NewEmpty(to)
	}
	if v, ok := vec.(*vector.Const); ok {
		return constOf(c.cast(v.Any, to), n)
	}
	if optionType, ok := to.(*super.TypeOption); ok {
		return c.toOption(vec, optionType)
	}
	from := vec.Type()
	switch {
	case from == to:
		return vec
	case from.ID() == to.ID():
		// The types have the same underlying type.
		return retype(vec, to)
	case from == super.TypeNull:
		return vector.NewUnionOfOne(c.sctx.Nullable(to), vector.NewNull(n))
	case from == super.TypeNone:
		// Casting none to a non-option type T is shorthand for casting
		// it to option(T) except for a union with a none member, which
		// is handled by toUnion below.
		if super.TypeUnder(to) == super.TypeNone {
			return vector.NewNone(n)
		}
		if !unionHas(to, super.TypeNone) {
			return vector.NewOptionNone(c.sctx.LookupTypeOption(to), n)
		}
	}
	switch to := to.(type) {
	case *super.TypeRecord:
		return c.toRecord(vec, to)
	case *super.TypeArray:
		return c.toList(vec, to, to.Type)
	case *super.TypeSet:
		return c.toList(vec, to, to.Type)
	case *super.TypeMap:
		return c.toMap(vec, to)
	case *super.TypeUnion:
		return c.toUnion(vec, to)
	case *super.TypeError:
		return c.wrap(c.cast(vec, to.Type), to.Type, func(vec vector.Any) vector.Any {
			return vector.NewError(to, vec)
		})
	case *super.TypeNamed:
		return c.wrap(c.cast(vec, to.Type), to.Type, func(vec vector.Any) vector.Any {
			return vector.NewNamed(to, vec)
		})
	case *super.TypeFusion:
		if to.Type != super.TypeAll {
			return c.error(vec, to)
		}
		// The values of a fusion(all) are the encoded values of their
		// subtypes.
		subtypes := make([]super.Type, n)
		b := vector.NewValueBuilder(super.TypeAll)
		var sb scode.Builder
		for i := range n {
			subtypes[i] = from
			sb.Truncate()
			vec.Serialize(&sb, i)
			b.Write(sb.Bytes().Body())
		}
		return vector.NewFusion(c.sctx, to, b.Build(c.sctx), subtypes)
	case *super.TypeDecimal, *super.TypeOfUnimplemented:
		return vector.NewWrappedError(c.sctx, "cannot cast to "+sup.FormatType(to)+": type not yet implemented", vec)
	case *super.TypeEnum:
		return c.toEnum(vec, to)
	}
	return To(c.sctx, vec, to)
}

// constOf returns a vector of n copies of the single value in vec.  Since
// only primitive values may be consts, unions, options, named types, errors,
// and the unit types are returned as themselves over a const, fusion(all)
// values are copied, and other complex values are repeated with a view.
func constOf(vec vector.Any, n uint32) vector.Any {
	switch v := vec.(type) {
	case *vector.Dynamic:
		return constOf(v.Values[v.Tags[0]], n)
	case *vector.Union:
		tag := v.Tags()[0]
		tags := make([]uint32, n)
		for k := range tags {
			tags[k] = tag
		}
		vals := slices.Clone(v.Values())
		vals[tag] = constOf(vals[tag], n)
		return vector.NewUnion(v.Typ, tags, vals)
	case *vector.Option:
		if _, ok := v.Any.(*vector.None); ok {
			return vector.NewOptionNone(v.Typ, n)
		}
		return vector.NewOptionSome(v.Typ, constOf(v.Any, n))
	case *vector.Named:
		return vector.NewNamed(v.Typ, constOf(v.Any, n))
	case *vector.Error:
		return vector.NewError(v.Typ, constOf(v.Vals, n))
	case *vector.Fusion:
		values, ok := v.Values.(*vector.Bytes)
		if !ok {
			break
		}
		bytes := values.Value(0)
		ids := v.Subtypes.RawIDs()
		b := vector.NewValueBuilder(super.TypeAll)
		subtypes := make([]uint32, n)
		for k := range subtypes {
			b.Write(bytes)
			subtypes[k] = ids[0]
		}
		return &vector.Fusion{Typ: v.Typ, Values: b.Build(nil), Subtypes: v.Subtypes.WithTypeIDs(subtypes)}
	case *vector.Null:
		return vector.NewNull(n)
	case *vector.None:
		return vector.NewNone(n)
	}
	if vec.Type().ID() < super.IDTypeComplex {
		return vector.NewConst(vec, n)
	}
	// Only primitive values may be consts so a complex value is repeated
	// with a view.
	return vector.Pick(vec, make([]uint32, n))
}

// wrap applies fn to the values of vec of type typ, leaving the others (e.g.,
// errors from a failed cast) as is.
func (c *complexCaster) wrap(vec vector.Any, typ super.Type, fn func(vector.Any) vector.Any) vector.Any {
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		if vecs[0].Type() != typ {
			return vecs[0]
		}
		return fn(vecs[0])
	}, vec)
}

func (c *complexCaster) toOption(vec vector.Any, to *super.TypeOption) vector.Any {
	n := vec.Len()
	if o, ok := vector.Under(vec).(*vector.Option); ok {
		// vec is a pure some or a pure none since cast rips options.
		if _, ok := o.Any.(*vector.None); ok {
			return vector.NewOptionNone(to, n)
		}
		vec = o.Any
	}
	switch super.TypeUnder(vec.Type()) {
	case super.TypeNone:
		// Casting none (e.g., from a field of type T|none) to an option
		// type yields that option's none.
		return vector.NewOptionNone(to, n)
	case super.TypeNull:
		// Casting null to an option type whose type doesn't admit null
		// yields the option's none.  (Otherwise, null is cast to the
		// option's type, as for any other value.)
		if !AdmitsNull(to.Type) {
			return vector.NewOptionNone(to, n)
		}
	}
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		vec := vecs[0]
		if vec.Type() == to.Type {
			return vector.NewOptionSome(to, vec)
		}
		if HasError(vec.Type()) {
			// As with the sam caster, a cast that failed (in part) isn't
			// an option.
			return vec
		}
		return vector.NewOptionSome(c.sctx.LookupTypeOption(vec.Type()), vec)
	}, c.cast(vec, to.Type))
}

func (c *complexCaster) error(vec vector.Any, to super.Type) vector.Any {
	return vector.NewWrappedError(c.sctx, "cannot cast to "+sup.FormatType(to), vec)
}

func (c *complexCaster) toEnum(vec vector.Any, to *super.TypeEnum) vector.Any {
	if vec.Type().ID() != super.IDString {
		return vector.NewWrappedError(c.sctx, "cannot cast to enum", vec)
	}
	var selectors []uint64
	var errs []uint32
	var b scode.Builder
	for i := range vec.Len() {
		val := vector.ValueAt(&b, vec, i)
		if selector := to.Lookup(super.DecodeString(val.Bytes())); selector >= 0 {
			selectors = append(selectors, uint64(selector))
		} else {
			errs = append(errs, i)
		}
	}
	out := vector.NewEnum(to, selectors)
	if len(errs) == 0 {
		return out
	}
	errVec := vector.NewWrappedError(c.sctx, "no such symbol in "+sup.String(to), vector.Pick(vec, errs))
	if len(selectors) == 0 {
		return errVec
	}
	return vector.Combine(out, errs, errVec)
}

func (c *complexCaster) toRecord(vec vector.Any, to *super.TypeRecord) vector.Any {
	rec, ok := under(vec).(*vector.Record)
	if !ok {
		if m, ok := under(vec).(*vector.Map); ok && isStringKeyType(m.Typ.KeyType) {
			return c.mapToRecord(vec, m, to)
		}
		return c.error(vec, to)
	}
	n := rec.Len()
	if c.opts.Strict {
		for k, f := range rec.Typ.Fields {
			if !to.HasField(f.Name) && super.TypeUnder(rec.Fields[k].Type()) != super.TypeNone {
				return vector.NewWrappedError(c.sctx, "cannot cast to "+sup.FormatType(to)+": no field "+sup.QuotedName(f.Name)+" in type", vec)
			}
		}
	}
	if len(to.Fields) == 0 {
		return vector.NewRecord(to, nil, n)
	}
	var fields []vector.Any
	for _, f := range to.Fields {
		k, ok := rec.Typ.IndexOfField(f.Name)
		if !ok {
			fields = append(fields, c.absentField(f, n))
			continue
		}
		field := rec.Fields[k]
		if super.TypeUnder(field.Type()) == super.TypeNone {
			// A field of type none is absent (but a none value of a
			// union type like T|none is cast like any other value).
			fields = append(fields, c.absentField(f, n))
			continue
		}
		fields = append(fields, c.cast(field, f.Type))
	}
	return c.buildRecord(to, fields)
}

// buildRecord returns the record of type to with the cast field values in
// fields, whose types may differ from the types of the fields of to (e.g.,
// for an error from a failed cast).
func (c *complexCaster) buildRecord(to *super.TypeRecord, fields []vector.Any) vector.Any {
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		typ := to
		for k, vec := range vecs {
			if vec.Type() != to.Fields[k].Type {
				fields := make([]super.Field, len(vecs))
				for k, vec := range vecs {
					fields[k] = super.NewField(to.Fields[k].Name, vec.Type())
				}
				typ = c.sctx.MustLookupTypeRecord(fields)
				break
			}
		}
		return vector.NewRecord(typ, slices.Clone(vecs), vecs[0].Len())
	}, fields...)
}

// absentField returns the value of field f for n records that don't have
// the field, which is the none of an option, null if the AbsentToNull
// option is set and the field's type admits null, or else an error.
func (c *complexCaster) absentField(f super.Field, n uint32) vector.Any {
	if optionType, ok := f.Type.(*super.TypeOption); ok {
		return vector.NewOptionNone(optionType, n)
	}
	if c.opts.AbsentToNull {
		if f.Type == super.TypeNull {
			return vector.NewNull(n)
		}
		if union, ok := f.Type.(*super.TypeUnion); ok {
			if tag := union.TagOf(super.TypeNull); tag >= 0 {
				return c.unionOf(vector.NewNull(n), union, tag)
			}
		}
	}
	return vector.NewStringError(c.sctx, "no such field "+sup.QuotedName(f.Name), n)
}

func (c *complexCaster) toList(vec vector.Any, to, toInner super.Type) vector.Any {
	var offsets []uint32
	var vals vector.Any
	switch v := under(vec).(type) {
	case *vector.Array:
		offsets, vals = v.Offsets, v.Values
	case *vector.Set:
		offsets, vals = v.Offsets, v.Values
	default:
		return c.error(vec, to)
	}
	elems := newElems(c.sctx, c.cast(vals, toInner))
	_, isSet := to.(*super.TypeSet)
	if elems.tags == nil && elems.pieces[0].Type() == toInner {
		// Every element was cast to toInner.
		out := newList(to, offsets, elems.pieces[0])
		if isSet {
			out = c.normalize(out, super.NormalizeSet)
		}
		return out
	}
	groups := map[string]*listGroup{}
	var order []*listGroup
	rowGroups := make([]uint32, len(offsets)-1)
	for row := range rowGroups {
		lo, hi := offsets[row], offsets[row+1]
		key := elems.key(lo, hi)
		g, ok := groups[key]
		if !ok {
			g = &listGroup{tag: uint32(len(order)), offsets: []uint32{0}}
			groups[key] = g
			order = append(order, g)
		}
		for i := lo; i < hi; i++ {
			g.index = append(g.index, i)
		}
		g.offsets = append(g.offsets, uint32(len(g.index)))
		rowGroups[row] = g.tag
	}
	var outs []vector.Any
	for _, g := range order {
		var out vector.Any
		if len(g.index) == 0 {
			// As with the sam caster, empty containers have the target type.
			out = newList(to, g.offsets, vector.NewEmpty(toInner))
		} else {
			inner, vals := elems.build(g.index, toInner)
			typ := to
			if inner != toInner {
				if isSet {
					typ = c.sctx.LookupTypeSet(inner)
				} else {
					typ = c.sctx.LookupTypeArray(inner)
				}
			}
			out = newList(typ, g.offsets, vals)
			if isSet {
				out = c.normalize(out, super.NormalizeSet)
			}
		}
		outs = append(outs, out)
	}
	if len(outs) == 1 {
		return outs[0]
	}
	return vector.NewDynamic(rowGroups, outs)
}

type listGroup struct {
	tag     uint32
	index   []uint32
	offsets []uint32
}

func newList(typ super.Type, offsets []uint32, vals vector.Any) vector.Any {
	if typ, ok := typ.(*super.TypeSet); ok {
		return vector.NewSet(typ, offsets, vals)
	}
	return vector.NewArray(typ.(*super.TypeArray), offsets, vals)
}

func (c *complexCaster) toMap(vec vector.Any, to *super.TypeMap) vector.Any {
	m, ok := under(vec).(*vector.Map)
	if !ok {
		if rec, ok := under(vec).(*vector.Record); ok && super.TypeUnder(to.KeyType) == super.TypeString {
			return c.recordToMap(rec, to)
		}
		return c.error(vec, to)
	}
	keys := newElems(c.sctx, c.cast(m.Keys, to.KeyType))
	vals := newElems(c.sctx, c.cast(m.Values, to.ValType))
	if keys.tags == nil && vals.tags == nil && keys.pieces[0].Type() == to.KeyType && vals.pieces[0].Type() == to.ValType {
		// Every key and value was cast to the target types.
		return c.normalize(vector.NewMap(to, m.Offsets, keys.pieces[0], vals.pieces[0]), super.NormalizeMap)
	}
	type mapGroup struct {
		tag     uint32
		index   []uint32
		offsets []uint32
	}
	groups := map[string]*mapGroup{}
	var order []*mapGroup
	rowGroups := make([]uint32, len(m.Offsets)-1)
	for row := range rowGroups {
		lo, hi := m.Offsets[row], m.Offsets[row+1]
		key := keys.key(lo, hi) + "/" + vals.key(lo, hi)
		g, ok := groups[key]
		if !ok {
			g = &mapGroup{tag: uint32(len(order)), offsets: []uint32{0}}
			groups[key] = g
			order = append(order, g)
		}
		for i := lo; i < hi; i++ {
			g.index = append(g.index, i)
		}
		g.offsets = append(g.offsets, uint32(len(g.index)))
		rowGroups[row] = g.tag
	}
	var outs []vector.Any
	for _, g := range order {
		var out vector.Any
		if len(g.index) == 0 {
			out = vector.NewMap(to, g.offsets, vector.NewEmpty(to.KeyType), vector.NewEmpty(to.ValType))
		} else {
			keyType, keyVals := keys.build(g.index, to.KeyType)
			valType, valVals := vals.build(g.index, to.ValType)
			typ := to
			if keyType != to.KeyType || valType != to.ValType {
				typ = c.sctx.LookupTypeMap(keyType, valType)
			}
			out = c.normalize(vector.NewMap(typ, g.offsets, keyVals, valVals), super.NormalizeMap)
		}
		outs = append(outs, out)
	}
	if len(outs) == 1 {
		return outs[0]
	}
	return vector.NewDynamic(rowGroups, outs)
}

// normalize applies fn (super.NormalizeSet or super.NormalizeMap) to the body
// of each value of vec, a set or map vector, since casting the elements may
// change their order or make them equal.
func (c *complexCaster) normalize(vec vector.Any, fn func(scode.Bytes) scode.Bytes) vector.Any {
	b := vector.NewValueBuilder(vec.Type())
	var sb scode.Builder
	for i := range vec.Len() {
		sb.Truncate()
		vec.Serialize(&sb, i)
		bytes := sb.Bytes().Body()
		if bytes != nil {
			bytes = fn(bytes)
		}
		b.Write(bytes)
	}
	return b.Build(c.sctx)
}

func (c *complexCaster) toUnion(vec vector.Any, to *super.TypeUnion) vector.Any {
	from := vec.Type()
	tag := bestUnionTag(from, to)
	if tag < 0 {
		// A value is cast to a union of a single type and none or null
		// (e.g., T|none as generated by infer or T|null as with a SQL
		// column) by casting it to the single type.
		if inner, ok := singletonUnionNullish(to.Types); ok {
			return vector.Apply(vector.ApplyRipUnions, func(vecs ...vector.Any) vector.Any {
				vec := vecs[0]
				if tag := bestUnionTag(vec.Type(), to); tag >= 0 {
					return c.unionOf(vec, to, tag)
				}
				return vec
			}, c.cast(vec, inner))
		}
		if tag = super.BestUnionMatch(from, to); tag < 0 {
			return c.error(vec, to)
		}
		return c.unionOf(c.cast(vec, to.Types[tag]), to, tag)
	}
	return c.unionOf(vec, to, tag)
}

// unionOf returns the values of vec as values of the union type to with the
// tag tag.  Values of vec not of the tag's type (e.g., errors from a failed
// cast) are left as is.
func (c *complexCaster) unionOf(vec vector.Any, to *super.TypeUnion, tag int) vector.Any {
	member := to.Types[tag]
	return vector.Apply(vector.ApplyNone, func(vecs ...vector.Any) vector.Any {
		vec := vecs[0]
		if super.TypeUnder(vec.Type()) != super.TypeUnder(member) {
			return vec
		}
		vec = retype(vec, member)
		tags := make([]uint32, vec.Len())
		for k := range tags {
			tags[k] = uint32(tag)
		}
		vals := make([]vector.Any, len(to.Types))
		for k, typ := range to.Types {
			if k == tag {
				vals[k] = vec
			} else {
				vals[k] = vector.NewEmpty(typ)
			}
		}
		return vector.NewUnion(to, tags, vals)
	}, vec)
}

// retype returns vec as a vector of typ, which has the same underlying type.
func retype(vec vector.Any, typ super.Type) vector.Any {
	if vec.Type() == typ {
		return vec
	}
	vec = vector.Under(vec)
	if named, ok := typ.(*super.TypeNamed); ok {
		return vector.NewNamed(named, retype(vec, named.Type))
	}
	return vec
}

// under returns the underlying vector of vec with any named types and views
// removed.
func under(vec vector.Any) vector.Any {
	return vector.Under(vector.PushView(vector.Under(vec)))
}

func unionHas(typ, member super.Type) bool {
	union, ok := super.TypeUnder(typ).(*super.TypeUnion)
	return ok && slices.Contains(union.Types, member)
}

// singletonUnionNullish returns T if types are T and none or null.
func singletonUnionNullish(types []super.Type) (super.Type, bool) {
	if len(types) == 2 {
		if types[0] == super.TypeNone || types[0] == super.TypeNull {
			return types[1], true
		}
		if types[1] == super.TypeNone || types[1] == super.TypeNull {
			return types[0], true
		}
	}
	return nil, false
}

// bestUnionTag tries to return the most specific union tag for in
// within out.  See the sam caster.
func bestUnionTag(in, out super.Type) int {
	outUnion, ok := super.TypeUnder(out).(*super.TypeUnion)
	if !ok {
		return -1
	}
	typeUnderIn := super.TypeUnder(in)
	underlying := -1
	compatible := -1
	for i, t := range outUnion.Types {
		if t == in {
			return i
		}
		if t == typeUnderIn && underlying == -1 {
			underlying = i
		}
		if super.TypeUnder(t) == typeUnderIn && compatible == -1 {
			compatible = i
		}
	}
	if underlying != -1 {
		return underlying
	}
	return compatible
}

// HasError returns true if typ is or contains an error type, which for a
// cast result means the cast failed in whole or in part.
func HasError(typ super.Type) bool {
	switch typ := super.TypeUnder(typ).(type) {
	case *super.TypeError:
		return true
	case *super.TypeRecord:
		return slices.ContainsFunc(typ.Fields, func(f super.Field) bool { return HasError(f.Type) })
	case *super.TypeArray:
		return HasError(typ.Type)
	case *super.TypeSet:
		return HasError(typ.Type)
	case *super.TypeMap:
		return HasError(typ.KeyType) || HasError(typ.ValType)
	case *super.TypeUnion:
		return slices.ContainsFunc(typ.Types, HasError)
	case *super.TypeOption:
		return HasError(typ.Type)
	}
	return false
}

// elems describes the cast elements of a set of containers (or the keys or
// values of a set of maps) so that the containers can be rebuilt with the
// element type of each container determined by the types of its elements.
type elems struct {
	sctx   *super.Context
	vec    vector.Any
	pieces []vector.Any
	// tags[i] is the piece of element i and local[i] its index in the
	// piece.  tags is nil if there is a single piece.
	tags  []uint32
	local []uint32
}

func newElems(sctx *super.Context, vec vector.Any) *elems {
	e := &elems{sctx: sctx, vec: vec}
	if d, ok := vec.(*vector.Dynamic); ok {
		e.pieces = d.Values
		e.tags = d.Tags
		e.local = d.ForwardTagMap()
	} else {
		e.pieces = []vector.Any{vec}
	}
	return e
}

func (e *elems) tag(i uint32) uint32 {
	if e.tags == nil {
		return 0
	}
	return e.tags[i]
}

func (e *elems) localIndex(i uint32) uint32 {
	if e.tags == nil {
		return i
	}
	return e.local[i]
}

// key returns a string identifying the set of piece types of elements
// lo through hi-1.
func (e *elems) key(lo, hi uint32) string {
	if lo == hi {
		return "empty"
	}
	if e.tags == nil {
		return "0"
	}
	var tags []uint32
	for i := lo; i < hi; i++ {
		if t := e.tags[i]; !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	slices.Sort(tags)
	var b strings.Builder
	for _, t := range tags {
		b.WriteString(strconv.Itoa(int(t)))
		b.WriteByte(',')
	}
	return b.String()
}

// build returns the element type and element vector for the elements in
// index (which all have the same set of piece types).  As with the sam
// complexCaster, the element type is the type of the elements if they have a
// single type and otherwise the union of the (flattened) types, which is
// the same as toInner when the elements were all cast successfully to
// toInner.
func (e *elems) build(index []uint32, toInner super.Type) (super.Type, vector.Any) {
	var pieceTags []uint32
	for _, i := range index {
		if t := e.tag(i); !slices.Contains(pieceTags, t) {
			pieceTags = append(pieceTags, t)
		}
	}
	var types []super.Type
	for _, t := range pieceTags {
		types = append(types, e.pieces[t].Type())
	}
	flat := super.Flatten(types)
	if len(flat) == 1 && len(types) == 1 && types[0] == flat[0] {
		// The elements all come from one piece.
		local := make([]uint32, len(index))
		for k, i := range index {
			local[k] = e.localIndex(i)
		}
		return flat[0], vector.Pick(e.pieces[pieceTags[0]], local)
	}
	if len(flat) == 1 {
		// The elements come from several pieces of the same type.
		return flat[0], e.concat(flat[0], func(yield func(vector.Any, uint32)) {
			for _, i := range index {
				yield(e.pieces[e.tag(i)], e.localIndex(i))
			}
		}, len(index))
	}
	union := e.sctx.MustLookupTypeUnion(flat)
	// Each element becomes a value of the union, whose type is the
	// element's own type with any union removed.
	type source struct {
		vec   vector.Any
		index uint32
	}
	members := make([][]source, len(union.Types))
	tags := make([]uint32, len(index))
	for k, i := range index {
		vec, slot := e.member(e.pieces[e.tag(i)], e.localIndex(i))
		tag := union.TagOf(vec.Type())
		tags[k] = uint32(tag)
		members[tag] = append(members[tag], source{vec, slot})
	}
	vals := make([]vector.Any, len(union.Types))
	for tag, srcs := range members {
		vals[tag] = e.concat(union.Types[tag], func(yield func(vector.Any, uint32)) {
			for _, s := range srcs {
				yield(s.vec, s.index)
			}
		}, len(srcs))
	}
	return union, vector.NewUnion(union, tags, vals)
}

// member returns the vector and slot of the value at slot of vec with any
// unions removed (as with super.Flatten, named unions are not removed).
func (e *elems) member(vec vector.Any, slot uint32) (vector.Any, uint32) {
	for {
		u, ok := vec.(*vector.Union)
		if !ok {
			return vec, slot
		}
		d := u.Dynamic()
		vec, slot = d.Values[d.Tags[slot]], d.ForwardTagMap()[slot]
	}
}

// concat returns a vector of typ with the n values from the sources in the
// order given, using a Pick when there's a single source.
func (e *elems) concat(typ super.Type, sources func(func(vector.Any, uint32)), n int) vector.Any {
	if n == 0 {
		return vector.NewEmpty(typ)
	}
	var vec vector.Any
	var index []uint32
	single := true
	sources(func(v vector.Any, slot uint32) {
		if vec == nil {
			vec = v
		} else if v != vec {
			single = false
		}
		index = append(index, slot)
	})
	if single {
		return vector.Pick(vec, index)
	}
	b := vector.NewValueBuilder(typ)
	var sb scode.Builder
	sources(func(v vector.Any, slot uint32) {
		sb.Truncate()
		v.Serialize(&sb, slot)
		b.Write(sb.Bytes().Body())
	})
	return b.Build(e.sctx)
}

// AdmitsNull returns true if typ is null or a union with a null member.
func AdmitsNull(typ super.Type) bool {
	if union, ok := super.TypeUnder(typ).(*super.TypeUnion); ok {
		return slices.ContainsFunc(union.Types, AdmitsNull)
	}
	return super.TypeUnder(typ) == super.TypeNull
}

// recordToMap casts rec to the map type to, whose key type is string, with an
// entry for each field of rec whose key is the field's name and whose value
// is the field's value cast to the map's value type.  An absent optional
// field or a field of type none has no entry.
func (c *complexCaster) recordToMap(rec *vector.Record, to *super.TypeMap) vector.Any {
	n := rec.Len()
	// For each field, vals is its present values and present tells whether
	// each row has the field (or is nil if every row does).
	vals := make([]vector.Any, len(rec.Fields))
	present := make([][]bool, len(rec.Fields))
	for k, f := range rec.Fields {
		if super.TypeUnder(f.Type()) == super.TypeNone {
			// A field of type none is absent and has no entry (as
			// when casting a record to a record).
			continue
		}
		vals[k] = f
		o, ok := vector.Under(vector.PushView(f)).(*vector.Option)
		if !ok {
			continue
		}
		p := make([]bool, n)
		switch any := o.Any.(type) {
		case *vector.None:
			vals[k] = nil
		case *vector.Dynamic:
			for i, tag := range any.Tags {
				p[i] = tag == super.OptionSomeTag
			}
			vals[k] = any.Values[super.OptionSomeTag]
		default:
			for i := range p {
				p[i] = true
			}
			vals[k] = any
		}
		present[k] = p
	}
	offsets := []uint32{0}
	keys := vector.NewStringEmpty(0)
	var tags []uint32
	for i := range n {
		for k, f := range rec.Typ.Fields {
			if vals[k] == nil || present[k] != nil && !present[k][i] {
				continue
			}
			keys.Append(f.Name)
			tags = append(tags, uint32(k))
		}
		offsets = append(offsets, uint32(len(tags)))
	}
	if len(tags) == 0 {
		return vector.NewMap(to, offsets, vector.NewEmpty(to.KeyType), vector.NewEmpty(to.ValType))
	}
	for k, val := range vals {
		if val == nil {
			// The values of a dynamic must be non-nil.
			vals[k] = vector.NewEmpty(super.TypeNull)
		}
	}
	values := vbuild.MergeSameTypesInDynamic(vector.FlattenUnions(vector.NewDynamic(tags, vals)))
	if d, ok := values.(*vector.Dynamic); ok {
		values = vector.NewUnionFromDynamic(c.sctx, d)
	}
	m := vector.NewMap(c.sctx.LookupTypeMap(super.TypeString, values.Type()), offsets, keys, values)
	return c.toMap(m, to)
}

// mapToRecord casts m, a map with string keys, to the record type to, where
// the value of each field of to is the value of the map entry whose key is
// the field's name cast to the field's type.  As when casting a record to a
// record, an entry whose key isn't a field of to is omitted (or, with the
// Strict option, makes the map an error) and a field with no entry is absent
// (see absentField).  If a key appears more than once, its first entry is
// used.
func (c *complexCaster) mapToRecord(vec vector.Any, m *vector.Map, to *super.TypeRecord) vector.Any {
	n := m.Len()
	which := make(map[string]int, len(to.Fields))
	for k, f := range to.Fields {
		which[f.Name] = k
	}
	// The key type may be a named string type.
	keys := vector.Under(m.Keys)
	if c.opts.Strict {
		if out, ok := c.strictMapToRecord(vec, m, keys, which, to); ok {
			return out
		}
	}
	// For each field, index is the positions in m.Values of its entries
	// and absent is the rows with no entry for it.
	index := make([][]uint32, len(to.Fields))
	absent := make([][]uint32, len(to.Fields))
	found := make([]bool, len(to.Fields))
	for row := range n {
		clear(found)
		for i := m.Offsets[row]; i < m.Offsets[row+1]; i++ {
			k, ok := which[vector.StringValue(keys, i)]
			if !ok || found[k] {
				// Skip a key that isn't a field or that's a duplicate.
				continue
			}
			index[k] = append(index[k], i)
			found[k] = true
		}
		for k := range to.Fields {
			if !found[k] {
				absent[k] = append(absent[k], row)
			}
		}
	}
	if len(to.Fields) == 0 {
		return vector.NewRecord(to, nil, n)
	}
	fields := make([]vector.Any, 0, len(to.Fields))
	for k, f := range to.Fields {
		var field vector.Any
		switch {
		case len(absent[k]) == int(n):
			field = c.absentField(f, n)
		case len(absent[k]) == 0:
			field = c.cast(vector.Pick(m.Values, index[k]), f.Type)
		default:
			present := c.cast(vector.Pick(m.Values, index[k]), f.Type)
			field = vector.Combine(present, absent[k], c.absentField(f, uint32(len(absent[k]))))
		}
		fields = append(fields, field)
	}
	return c.buildRecord(to, fields)
}

// strictMapToRecord handles the Strict option for mapToRecord.  Each map in
// m with a key that isn't a field of to is an error, as with a record with a
// field that isn't in to, and the other maps are cast to to.  Since keys vary
// per map (unlike the fields of a record vector), this is decided per map.
// If no map has such a key, strictMapToRecord returns false.
func (c *complexCaster) strictMapToRecord(vec vector.Any, m *vector.Map, keys vector.Any, which map[string]int, to *super.TypeRecord) (vector.Any, bool) {
	n := m.Len()
	var good []uint32
	// tags is 0 for a map that is cast and otherwise 1 plus the index in
	// msgs of its error message.
	tags := make([]uint32, n)
	var msgs []string
	rows := map[string][]uint32{}
	for row := range n {
		var bad string
		var isBad bool
		for i := m.Offsets[row]; i < m.Offsets[row+1]; i++ {
			if key := vector.StringValue(keys, i); !hasKey(which, key) {
				bad, isBad = key, true
				break
			}
		}
		if !isBad {
			good = append(good, row)
			continue
		}
		msg := "cannot cast to " + sup.FormatType(to) + ": no field " + sup.QuotedName(bad) + " in type"
		if _, ok := rows[msg]; !ok {
			msgs = append(msgs, msg)
		}
		rows[msg] = append(rows[msg], row)
		tags[row] = uint32(slices.Index(msgs, msg) + 1)
	}
	if len(msgs) == 0 {
		return nil, false
	}
	vals := make([]vector.Any, 0, len(msgs)+1)
	if len(good) != 0 {
		// These maps have no such keys so they're cast normally (and
		// nested values are still cast strictly).
		vals = append(vals, c.cast(vector.Pick(vec, good), to))
	} else {
		// The values of a dynamic must be non-nil.
		vals = append(vals, vector.NewEmpty(to))
	}
	for _, msg := range msgs {
		vals = append(vals, vector.NewWrappedError(c.sctx, msg, vector.Pick(vec, rows[msg])))
	}
	return vector.NewDynamic(tags, vals), true
}

func hasKey(which map[string]int, key string) bool {
	_, ok := which[key]
	return ok
}

// isStringKeyType returns true if typ is string or none, the key type of an
// empty map literal.
func isStringKeyType(typ super.Type) bool {
	typ = super.TypeUnder(typ)
	return typ == super.TypeString || typ == super.TypeNone
}
