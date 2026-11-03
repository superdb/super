package super

// BestUnionMatch returns the tag of the member of union that best matches
// the structure of the record, array, set, or map type in, or -1 if in is
// not such a type or no member matches.  It is used by cast when in is not
// a member of union (nor compatible with one) so that, e.g., a record can
// be cast to a union of record types (such as a sum type of event types)
// by choosing the member it best fits.
//
// A member matches a record type if the member is a record type with every
// field of in and every member field not in in has an option type (so the
// cast yields none for it rather than an error).  Matching is applied
// recursively to nested records, arrays, sets, maps, and unions.  Among the
// matching members, the one with the most matching leaf types wins, with
// ties going to the lowest tag.
//
// A record type also matches a map type with string keys whose value type
// every field of the record matches, but only when no other member of the
// union matches, so a record is cast to a map only if it can't be cast to
// a record (e.g., for a recursive JSON type with a map{string:JSON} member).
func BestUnionMatch(in Type, union *TypeUnion) int {
	switch TypeUnder(in).(type) {
	case *TypeRecord, *TypeArray, *TypeSet, *TypeMap:
	default:
		return -1
	}
	tag, _, _ := bestMember(in, union.Types)
	return tag
}

// bestMember returns the tag and score of the member of types that best
// matches in (see BestUnionMatch) or -1 and false if none matches.
func bestMember(in Type, types []Type) (int, int, bool) {
	_, isRecord := TypeUnder(in).(*TypeRecord)
	for _, maps := range []bool{false, true} {
		if maps && !isRecord {
			break
		}
		best, bestScore := -1, -1
		for tag, member := range types {
			if isRecord && isMapType(member) != maps {
				continue
			}
			if score, ok := matchType(in, member); ok && score > bestScore {
				best, bestScore = tag, score
			}
		}
		if best >= 0 {
			return best, bestScore, true
		}
	}
	return -1, 0, false
}

func isMapType(typ Type) bool {
	if o, ok := typ.(*TypeOption); ok {
		typ = o.Type
	}
	_, ok := TypeUnder(typ).(*TypeMap)
	return ok
}

// matchType reports whether in can be cast to out structurally and, if so,
// returns a score counting the leaf types of in that match out exactly.
func matchType(in, out Type) (int, bool) {
	if in == out {
		return countLeaves(in), true
	}
	if option, ok := out.(*TypeOption); ok {
		return matchType(in, option.Type)
	}
	if named, ok := out.(*TypeNamed); ok {
		return matchType(in, named.Type)
	}
	if named, ok := in.(*TypeNamed); ok {
		return matchType(named.Type, out)
	}
	switch in := in.(type) {
	case *TypeOption:
		return matchType(in.Type, out)
	case *TypeUnion:
		// A union input matches if any of its members does.
		best, ok := -1, false
		for _, typ := range in.Types {
			if score, ok2 := matchType(typ, out); ok2 && score > best {
				best, ok = score, true
			}
		}
		return best, ok
	}
	if union, ok := out.(*TypeUnion); ok {
		_, score, ok := bestMember(in, union.Types)
		return score, ok
	}
	if in == TypeNull || in == TypeNone {
		// Null and none can be cast to anything.
		return 0, true
	}
	switch in := in.(type) {
	case *TypeRecord:
		if m, ok := out.(*TypeMap); ok {
			// A record matches a map with string keys whose value type
			// each field matches.
			if TypeUnder(m.KeyType) != TypeString {
				return 0, false
			}
			var score int
			for _, f := range in.Fields {
				s, ok := matchType(f.Type, m.ValType)
				if !ok {
					return 0, false
				}
				score += s
			}
			return score, true
		}
		out, ok := out.(*TypeRecord)
		if !ok {
			return 0, false
		}
		var score int
		for _, f := range in.Fields {
			k, ok := out.IndexOfField(f.Name)
			if !ok {
				return 0, false
			}
			s, ok := matchType(f.Type, out.Fields[k].Type)
			if !ok {
				return 0, false
			}
			score += s
		}
		for _, f := range out.Fields {
			if _, ok := in.IndexOfField(f.Name); !ok {
				if _, ok := f.Type.(*TypeOption); !ok {
					return 0, false
				}
			}
		}
		return score, true
	case *TypeArray:
		return matchInner(in.Type, out)
	case *TypeSet:
		return matchInner(in.Type, out)
	case *TypeMap:
		out, ok := out.(*TypeMap)
		if !ok {
			return 0, false
		}
		keyScore, ok := matchType(in.KeyType, out.KeyType)
		if !ok {
			return 0, false
		}
		valScore, ok := matchType(in.ValType, out.ValType)
		return keyScore + valScore, ok
	case *TypeError:
		out, ok := out.(*TypeError)
		if !ok {
			return 0, false
		}
		return matchType(in.Type, out.Type)
	}
	// in is a primitive type, which may be cast to another primitive
	// type (though the cast may fail for some values).
	return 0, IsPrimitiveType(out)
}

func matchInner(in, out Type) (int, bool) {
	switch out := out.(type) {
	case *TypeArray:
		return matchType(in, out.Type)
	case *TypeSet:
		return matchType(in, out.Type)
	}
	return 0, false
}

func countLeaves(typ Type) int {
	switch typ := typ.(type) {
	case *TypeNamed:
		return countLeaves(typ.Type)
	case *TypeOption:
		return countLeaves(typ.Type)
	case *TypeRecord:
		n := 0
		for _, f := range typ.Fields {
			n += countLeaves(f.Type)
		}
		return n
	case *TypeArray:
		return countLeaves(typ.Type)
	case *TypeSet:
		return countLeaves(typ.Type)
	case *TypeMap:
		return countLeaves(typ.KeyType) + countLeaves(typ.ValType)
	case *TypeError:
		return countLeaves(typ.Type)
	case *TypeUnion:
		n := 0
		for _, t := range typ.Types {
			n = max(n, countLeaves(t))
		}
		return n
	}
	return 1
}
