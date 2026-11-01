package demand

import (
	"maps"
	"slices"

	"github.com/superdb/super/pkg/field"
)

type Demand interface {
	isDemand()
}

func (demand all) isDemand()   {}
func (demand keys) isDemand()  {}
func (demand types) isDemand() {}

type all struct{}
type keys map[string]Demand // No empty values.

// types is the demand for the type of a value, i.e., the types of all of its
// fields (recursively) but none of their data, except that the data of the
// keys in the map are also needed.  The values in the map are always all or
// a non-empty types, as the type of a value implies the types of its fields.
type types map[string]Demand

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
	case types:
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
	case all, types:
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

// Delete deletes entries in b from a.
func Delete(a, b Demand) Demand {
	aa, ok := a.(keys)
	if !ok {
		return a
	}
	bb, ok := b.(keys)
	if !ok {
		return a
	}
	copyOnWrite := true
	for k, bv := range bb {
		av, ok := aa[k]
		if !ok {
			continue
		}
		if copyOnWrite {
			aa = maps.Clone(aa)
			copyOnWrite = false
		}
		if IsAll(bv) {
			delete(aa, k)
			continue
		}
		aa[k] = Delete(av, bv)
	}
	return aa
}

func Union(demands ...Demand) Demand {
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

func GetKey(demand Demand, key string) Demand {
	switch demand := demand.(type) {
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

// WithoutTypes returns d with each demand for types replaced with a demand
// for all of the data.
func WithoutTypes(d Demand) Demand {
	switch d := d.(type) {
	case all:
		return d
	case types:
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
	default:
		panic("Unreachable")
	}
	for k, v := range m {
		walk(v, append(path, k), data, typ)
	}
}
