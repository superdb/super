package super

// orderFields returns f.super with the fields of each record type ordered by
// rank aggregation over the field orders of the fused types.  For each
// record type of the supertype, the pairwise margin of fields x and y is the
// number of fused record types (at that position of the supertype) in which
// x precedes y minus the number in which y precedes x, and x beats y when
// the margin is positive.  Fields are ordered by repeatedly taking a field
// that no remaining field beats, preferring higher Copeland score (the
// number of fields a field beats minus the number it loses to), then higher
// net margin (the sum of its margins), then name (see fieldVotes.rank).  This
// preserves the field orders of the fused types when they agree and is
// independent of the order in which the types were fused.
//
// Votes are keyed by the supertype's record type rather than by its
// position(s) in the supertype since a record type has a single field order
// wherever it appears.
func (f *Fuser) orderFields() Type {
	o := &fieldOrderer{
		sctx:  f.sctx,
		votes: make(map[*TypeRecord]*fieldVotes),
		memo:  make(map[Type]Type),
	}
	for sub := range f.subtypes {
		o.vote(f.super, sub)
	}
	return o.reorder(f.super)
}

type fieldOrderer struct {
	sctx  *Context
	votes map[*TypeRecord]*fieldVotes
	memo  map[Type]Type
}

// fieldVotes holds the pairwise margins of the fields of a record type.
type fieldVotes struct {
	margins [][]int32
}

func (o *fieldOrderer) vote(super, sub Type) {
	sub = noFusion(sub)
	if union, ok := sub.(*TypeUnion); ok {
		if _, ok := super.(*TypeUnion); !ok {
			// The members of sub were fused into the single type super.
			for _, t := range union.Types {
				o.vote(super, t)
			}
			return
		}
	}
	switch super := super.(type) {
	case *TypeRecord:
		sub, ok := sub.(*TypeRecord)
		if !ok {
			return
		}
		o.voteRecord(super, sub)
		for _, field := range sub.Fields {
			if k, ok := super.IndexOfField(field.Name); ok {
				o.vote(super.Fields[k].Type, field.Type)
			}
		}
	case *TypeArray:
		if sub, ok := sub.(*TypeArray); ok {
			o.vote(super.Type, sub.Type)
		}
	case *TypeSet:
		if sub, ok := sub.(*TypeSet); ok {
			o.vote(super.Type, sub.Type)
		}
	case *TypeMap:
		if sub, ok := sub.(*TypeMap); ok {
			o.vote(super.KeyType, sub.KeyType)
			o.vote(super.ValType, sub.ValType)
		}
	case *TypeError:
		if sub, ok := sub.(*TypeError); ok {
			o.vote(super.Type, sub.Type)
		}
	case *TypeOption:
		if sub, ok := sub.(*TypeOption); ok {
			o.vote(super.Type, sub.Type)
		}
	case *TypeUnion:
		if sub, ok := sub.(*TypeUnion); ok {
			for _, t := range sub.Types {
				o.vote(super, t)
			}
			return
		}
		// A union of the supertype has at most one unnamed type of each
		// complex kind, into which sub was fused.
		if sub.Kind() == PrimitiveKind || IsTypeNamed(sub) {
			return
		}
		for _, t := range super.Types {
			if t.Kind() == sub.Kind() && !IsTypeNamed(t) {
				o.vote(t, sub)
				return
			}
		}
	}
	// Named types are barriers to fusion so their bodies aren't reordered.
}

func (o *fieldOrderer) voteRecord(super, sub *TypeRecord) {
	v, ok := o.votes[super]
	if !ok {
		n := len(super.Fields)
		v = &fieldVotes{margins: make([][]int32, n)}
		for i := range v.margins {
			v.margins[i] = make([]int32, n)
		}
		o.votes[super] = v
	}
	cols := make([]int, 0, len(sub.Fields))
	for _, field := range sub.Fields {
		if k, ok := super.IndexOfField(field.Name); ok {
			cols = append(cols, k)
		}
	}
	for i, x := range cols {
		for _, y := range cols[i+1:] {
			v.margins[x][y]++
			v.margins[y][x]--
		}
	}
}

// reorder returns typ with the fields of its record types ordered by the
// votes.
func (o *fieldOrderer) reorder(typ Type) Type {
	if out, ok := o.memo[typ]; ok {
		return out
	}
	var out Type
	switch typ := typ.(type) {
	case *TypeRecord:
		fields := make([]Field, len(typ.Fields))
		for k, field := range typ.Fields {
			fields[k] = NewField(field.Name, o.reorder(field.Type))
		}
		if v, ok := o.votes[typ]; ok {
			fields = v.rank(fields)
		}
		out = o.sctx.MustLookupTypeRecord(fields)
	case *TypeArray:
		out = o.sctx.LookupTypeArray(o.reorder(typ.Type))
	case *TypeSet:
		out = o.sctx.LookupTypeSet(o.reorder(typ.Type))
	case *TypeMap:
		out = o.sctx.LookupTypeMap(o.reorder(typ.KeyType), o.reorder(typ.ValType))
	case *TypeError:
		out = o.sctx.LookupTypeError(o.reorder(typ.Type))
	case *TypeOption:
		out = o.sctx.LookupTypeOption(o.reorder(typ.Type))
	case *TypeUnion:
		types := make([]Type, len(typ.Types))
		for k, t := range typ.Types {
			types[k] = o.reorder(t)
		}
		out = o.sctx.MustLookupTypeUnion(types)
	default:
		out = typ
	}
	o.memo[typ] = out
	return out
}

// rank returns fields, which are in the order of the record type for which
// v holds votes, ordered by the votes.  It repeatedly takes, from the fields
// not yet taken, the best field (by Copeland score, then net margin, then
// name, where scores are over the fields not yet taken) that no other such
// field beats.  When the majority relation has no cycles (e.g., the fused
// types agree on the relative order of the fields they share), no fused
// type's field order is contradicted.  When every remaining field is beaten
// (i.e., there is a cycle), the best remaining field is taken.  Scoring over
// the remaining fields keeps a field from being penalized for losing to a
// field that has already been taken.
func (v *fieldVotes) rank(fields []Field) []Field {
	n := len(fields)
	copeland := make([]int, n)
	net := make([]int, n)
	// beaten[i] is the number of remaining fields that beat field i.
	beaten := make([]int, n)
	for i := range n {
		for j, margin := range v.margins[i] {
			if j == i {
				continue
			}
			switch {
			case margin > 0:
				copeland[i]++
			case margin < 0:
				copeland[i]--
				beaten[i]++
			}
			net[i] += int(margin)
		}
	}
	better := func(i, j int) bool {
		if copeland[i] != copeland[j] {
			return copeland[i] > copeland[j]
		}
		if net[i] != net[j] {
			return net[i] > net[j]
		}
		return fields[i].Name < fields[j].Name
	}
	taken := make([]bool, n)
	out := make([]Field, 0, n)
	for range n {
		best, bestUnbeaten := -1, false
		for i := range n {
			if taken[i] {
				continue
			}
			unbeaten := beaten[i] == 0
			if best < 0 || (unbeaten && !bestUnbeaten) || (unbeaten == bestUnbeaten && better(i, best)) {
				best, bestUnbeaten = i, unbeaten
			}
		}
		taken[best] = true
		out = append(out, fields[best])
		// Remove best from the scores of the remaining fields.
		for j, margin := range v.margins[best] {
			if taken[j] {
				continue
			}
			switch {
			case margin > 0:
				beaten[j]--
				copeland[j]++
			case margin < 0:
				copeland[j]--
			}
			net[j] += int(margin)
		}
	}
	return out
}
