package demand

import (
	"maps"
	"slices"

	"github.com/superdb/super/pkg/field"
)

type Demand interface {
	isDemand()
}

func (demand all) isDemand()     {}
func (demand keys) isDemand()    {}
func (demand types) isDemand()   {}
func (demand asserts) isDemand() {}

type all struct{}
type keys map[string]Demand // No empty values.

// types is the demand for the type of a value, i.e., the types of all of its
// fields (recursively) but none of their data, except that the data of the
// keys in the map are also needed.  The values in the map are always all or
// a non-empty types, as the type of a value implies the types of its fields.
type types map[string]Demand

// asserts is the demand for a value that is used only through type
// assertions, e.g., this.(T).  Its keys are the IDs of the asserted types and
// each value is the demand for the value where it has that type (as a type
// assertion matches it).  The type of the value is needed (to evaluate the
// assertions) but its data is needed only where it has one of the types.
type asserts map[int]Demand

// Assert returns the demand for a value used only through a type assertion
// of the type with ID typeID, where d is the demand for the asserted value.
func Assert(typeID int, d Demand) Demand {
	return asserts{typeID: d}
}

func IsValid(demand Demand) bool {
	switch demand := demand.(type) {
	case nil:
		return false
	case all:
		return true
	case keys:
		for _, v := range demand {
			if !IsValid(v) || IsNone(v) {
				return false
			}
		}
		return true
	case types:
		for _, v := range demand {
			if !IsValid(v) || IsNone(v) {
				return false
			}
			if _, ok := v.(keys); ok {
				return false
			}
		}
		return true
	case asserts:
		for _, v := range demand {
			if !IsValid(v) {
				return false
			}
		}
		return true
	default:
		panic("Unreachable")
	}
}

func None() Demand {
	return keys{}
}

func All() Demand {
	return all{}
}

// TypesOnly returns the demand for just the type of a value.
func TypesOnly() Demand {
	return types{}
}

// Types returns the demand for the type of a value plus the data in d.
func Types(d Demand) Demand {
	switch d := d.(type) {
	case all:
		return d
	case keys:
		return toTypes(d)
	case types, asserts:
		// An assertion needs the type of the value.
		return d
	default:
		panic("Unreachable")
	}
}

func toTypes(m map[string]Demand) types {
	out := types{}
	for k, v := range m {
		if v = Types(v); !isTypesOnly(v) {
			// The types of the fields are implied by the type of the
			// value so a field that needs only types is dropped.
			out[k] = v
		}
	}
	return out
}

func isTypesOnly(d Demand) bool {
	t, ok := d.(types)
	return ok && len(t) == 0
}

func IsNone(demand Demand) bool {
	switch demand := demand.(type) {
	case all, types, asserts:
		return false
	case keys:
		return len(demand) == 0
	default:
		panic("Unreachable")
	}
}

func IsAll(demand Demand) bool {
	_, ok := demand.(all)
	return ok
}

func Key(key string, value Demand) Demand {
	if IsNone(value) {
		return value
	}
	return keys{key: value}
}

// Delete deletes entries in b from a.  If a is a demand for types, the
// types of the deleted entries are still needed (as part of the type of a)
// but their data is not.
func Delete(a, b Demand) Demand {
	bb, ok := b.(keys)
	if !ok {
		return a
	}
	var m map[string]Demand
	switch a := a.(type) {
	case keys:
		m = a
	case types:
		m = a
	default:
		return a
	}
	out := maps.Clone(m)
	for k, bv := range bb {
		av, ok := out[k]
		if !ok {
			continue
		}
		if IsAll(bv) {
			delete(out, k)
			continue
		}
		if av = Delete(av, bv); IsNone(av) || isTypesOnly(av) {
			delete(out, k)
		} else {
			out[k] = av
		}
	}
	if _, ok := a.(types); ok {
		return types(out)
	}
	return keys(out)
}

func Union(demands ...Demand) Demand {
	if d, ok := unionAsserts(demands); ok {
		return d
	}
	out := map[string]Demand{}
	var typed bool
	for _, d := range demands {
		var m map[string]Demand
		switch d := d.(type) {
		case all:
			return All()
		case keys:
			m = d
		case types:
			typed = true
			m = d
		case asserts:
			// The value is used other than through assertions.
			f, ok := flatten(d).(types)
			if !ok {
				return All()
			}
			typed = true
			m = f
		default:
			panic("Unreachable")
		}
		for k, v := range m {
			if v2, ok := out[k]; ok {
				out[k] = Union(v, v2)
			} else {
				out[k] = v
			}
		}
	}
	if typed {
		return toTypes(out)
	}
	return keys(out)
}

// unionAsserts returns the union of demands and true if each demand is for
// assertions, types only, or none (and at least one is for assertions).
func unionAsserts(demands []Demand) (Demand, bool) {
	var out asserts
	for _, d := range demands {
		switch d := d.(type) {
		case asserts:
			if out == nil {
				out = asserts{}
			}
			for id, v := range d {
				if v2, ok := out[id]; ok {
					v = Union(v, v2)
				}
				out[id] = v
			}
		default:
			// An assertion needs the type of the value so it subsumes
			// types only.
			if !IsNone(d) && !isTypesOnly(d) {
				return nil, false
			}
		}
	}
	return out, out != nil
}

// flatten returns the demand for the data of a used only through assertions
// when the value is also used otherwise: the type of the value and the data
// needed for every assertion.
func flatten(a asserts) Demand {
	d := TypesOnly()
	for _, v := range a {
		d = Union(d, Types(WithoutAsserts(v)))
	}
	return d
}

func GetKey(demand Demand, key string) Demand {
	switch demand := demand.(type) {
	case asserts:
		return GetKey(flatten(demand), key)
	case all:
		return demand
	case keys:
		if value, ok := demand[key]; ok {
			return value
		}
		return None()
	case types:
		if value, ok := demand[key]; ok {
			return value
		}
		return TypesOnly()
	default:
		panic("Unreachable")
	}
}

// WithoutTypes returns d with each demand for types (or for assertions)
// replaced with a demand for all of the data.
func WithoutTypes(d Demand) Demand {
	switch d := d.(type) {
	case all:
		return d
	case types, asserts:
		return All()
	case keys:
		out := keys{}
		for k, v := range d {
			out[k] = WithoutTypes(v)
		}
		return out
	default:
		panic("Unreachable")
	}
}

// WithoutAsserts returns d with each demand for assertions replaced with the
// demand for the type of the value and the data needed by every assertion.
func WithoutAsserts(d Demand) Demand {
	switch d := d.(type) {
	case all:
		return d
	case asserts:
		return WithoutAsserts(flatten(d))
	case keys:
		out := keys{}
		for k, v := range d {
			out[k] = WithoutAsserts(v)
		}
		return out
	case types:
		out := types{}
		for k, v := range d {
			out[k] = WithoutAsserts(v)
		}
		return toTypes(out)
	default:
		panic("Unreachable")
	}
}

// Projection returns the projection of the data and types needed by d, or
// nil if all of the data is needed.
func Projection(d Demand) field.Projection {
	if IsAll(d) {
		return nil
	}
	return field.NewProjectionMaybe(true, Fields(d), TypeFields(d), AssertProjection(d))
}

// AssertProjection returns the projection of the values needed only through
// assertions (see field.Projection), whose nodes lead to assertion nodes, or
// nil if there are no such values.  (The data and types needed by d otherwise
// are given by Fields and TypeFields.)  The projection of each assertion node
// needs the type of the asserted value so that the projected value keeps it
// for the assertion to match.
func AssertProjection(d Demand) field.Projection {
	var p field.Projection
	switch d := d.(type) {
	case asserts:
		for _, id := range slices.Sorted(maps.Keys(d)) {
			p = append(p, field.ProjectionNode{Assert: true, TypeID: id, Proj: Projection(Types(d[id]))})
		}
	case keys:
		p = keyAsserts(d)
	case types:
		p = keyAsserts(d)
	}
	return p
}

func keyAsserts(m map[string]Demand) field.Projection {
	var p field.Projection
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if proj := AssertProjection(m[k]); proj != nil {
			p = append(p, field.ProjectionNode{Name: k, Proj: proj})
		}
	}
	return p
}

// Fields returns the paths of the data needed by d.
func Fields(d Demand) []field.Path {
	var fields []field.Path
	walk(d, nil, &fields, nil)
	return fields
}

// TypeFields returns the paths of the values whose types are needed by d.
// (The types of the values in Fields are also needed but are not included.)
func TypeFields(d Demand) []field.Path {
	var fields []field.Path
	walk(d, nil, nil, &fields)
	return fields
}

func walk(d Demand, path field.Path, data, typ *[]field.Path) {
	var m map[string]Demand
	switch d := d.(type) {
	case all:
		if len(path) > 0 && data != nil {
			*data = append(*data, slices.Clone(path))
		}
		return
	case keys:
		m = d
	case types:
		if typ != nil {
			*typ = append(*typ, slices.Clone(path))
		}
		m = d
	case asserts:
		// See AssertProjection.
		return
	default:
		panic("Unreachable")
	}
	for k, v := range m {
		walk(v, append(path, k), data, typ)
	}
}
