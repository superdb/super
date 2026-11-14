package field

import "slices"

// A Projection is a tree of the record fields to be loaded from storage.
// A nil Projection means all of the data and an empty, non-nil Projection
// means none of the data.  The children of a ProjectionNode are the fields
// needed from the value at that node and a nil Proj means all of that value.
//
// A Projection may also contain a type node (a ProjectionNode with Types set),
// which indicates that the type of the value at that point in the projection
// is needed but that the data is needed only for the fields listed in the
// other nodes of the Projection.  The types of all the other fields are needed
// but not their data.  A Projection consisting of just a type node thus means
// that only the type of the value is needed.
//
// A Projection may instead consist of assertion nodes (ProjectionNodes with
// Assert set), which indicate that the value is used only through type
// assertions (e.g., this.(T)).  The type of the value is needed and, where it
// has the type of an assertion node (as a type assertion would match it), its
// data is projected with the node's Proj, but where it has none of the types,
// only its type is needed.
type Projection []ProjectionNode

type ProjectionNode struct {
	Name  string
	Proj  Projection
	Types bool
	// Assert is true for an assertion node, whose asserted type has
	// the ID TypeID: a DAG type ID in a DAG and a type ID of the query's
	// super.Context at runtime.
	Assert bool
	TypeID int
}

func NewProjection(paths []Path) Projection {
	var p Projection
	for _, path := range paths {
		p = p.insertPath(path)
	}
	return p
}

// NewProjectionMaybe returns the Projection for paths where only the types
// are needed for the values at typePaths and where asserts, which holds the
// paths to assertion nodes, is merged in.
func NewProjectionMaybe(hasProjection bool, paths, typePaths []Path, asserts Projection) Projection {
	if !hasProjection {
		return nil
	}
	if len(paths) == 0 && len(typePaths) == 0 && len(asserts) == 0 {
		return []ProjectionNode{}
	}
	p := NewProjection(paths)
	for _, path := range typePaths {
		p = p.insertTypePath(path)
	}
	return p.insertAsserts(asserts)
}

// insertAsserts merges q, whose nodes lead to assertion nodes, into p.
func (p Projection) insertAsserts(q Projection) Projection {
	for _, node := range q {
		if node.Assert {
			p = append(p, node)
			continue
		}
		i := p.index(node.Name)
		if i < 0 {
			p = append(p, node)
		} else if p[i].Proj != nil {
			// (If p[i].Proj is nil, all of the data is already needed.)
			p[i].Proj = p[i].Proj.insertAsserts(node.Proj)
		}
	}
	return p
}

func (p Projection) insertPath(path Path) Projection {
	if len(path) == 0 {
		return nil
	}
	i := p.index(path[0])
	if i < 0 {
		i = len(p)
		p = append(p, ProjectionNode{Name: path[0]})
	}
	p[i].Proj = p[i].Proj.insertPath(path[1:])
	return p
}

func (p Projection) insertTypePath(path Path) Projection {
	if len(path) == 0 {
		if !p.HasTypes() {
			p = append(p, ProjectionNode{Types: true})
		}
		return p
	}
	i := p.index(path[0])
	if i < 0 {
		i = len(p)
		p = append(p, ProjectionNode{Name: path[0]})
	} else if p[i].Proj == nil {
		// All of the data at this node is already needed.
		return p
	}
	p[i].Proj = p[i].Proj.insertTypePath(path[1:])
	return p
}

func (p Projection) index(name string) int {
	return slices.IndexFunc(p, func(n ProjectionNode) bool {
		return !n.Types && !n.Assert && n.Name == name
	})
}

// Lookup returns the projection of the node in p with the given name.
func (p Projection) Lookup(name string) (Projection, bool) {
	if i := p.index(name); i >= 0 {
		return p[i].Proj, true
	}
	return nil, false
}

// HasTypes returns true if p contains a type node.
func (p Projection) HasTypes() bool {
	return slices.ContainsFunc(p, func(n ProjectionNode) bool { return n.Types })
}

// HasAsserts returns true if p contains an assertion node.
func (p Projection) HasAsserts() bool {
	return slices.ContainsFunc(p, func(n ProjectionNode) bool { return n.Assert })
}

// Asserts returns the assertion nodes of p.
func (p Projection) Asserts() []ProjectionNode {
	var out []ProjectionNode
	for _, n := range p {
		if n.Assert {
			out = append(out, n)
		}
	}
	return out
}

// TypesOnly returns true if p consists of just a type node.
func (p Projection) TypesOnly() bool {
	return len(p) == 1 && p[0].Types
}

// NoTypes returns p with each node that needs only types (or has assertion
// nodes) replaced with a node that needs all of the data.  It is useful for
// readers that do not support type-only projections.
func (p Projection) NoTypes() Projection {
	if p.HasTypes() || p.HasAsserts() {
		return nil
	}
	if len(p) == 0 {
		return p
	}
	out := make(Projection, 0, len(p))
	for _, node := range p {
		out = append(out, ProjectionNode{Name: node.Name, Proj: node.Proj.NoTypes()})
	}
	return out
}

func (p Projection) Contains(q Projection) bool {
	if p == nil {
		// p can be considered to contain all columns.
		return true
	}
	if q == nil {
		// q can be considered to contain all columns.
		return false
	}
	pmap := map[string]Projection{}
	for _, node := range p {
		if !node.Types {
			pmap[node.Name] = node.Proj
		}
	}
	for _, qnode := range q {
		if qnode.Assert {
			// Don't bother comparing assertion nodes.
			return false
		}
		if qnode.Types {
			if !p.HasTypes() {
				return false
			}
			continue
		}
		pproj, ok := pmap[qnode.Name]
		if !ok || !pproj.Contains(qnode.Proj) {
			return false
		}
	}
	return true
}

func (p Projection) IsEmpty() bool {
	return p != nil && len(p) == 0
}

// Paths returns the paths of the data in p.  A node that contains a type node
// is treated as needing all of its data.  p must not contain a type node at
// its top level (see NoTypes).
func (p Projection) Paths() []Path {
	var paths []Path
	for _, node := range p {
		if node.Types || node.Assert {
			continue
		}
		name := node.Name
		if len(node.Proj) > 0 && !node.Proj.HasTypes() && !node.Proj.HasAsserts() {
			for _, path := range node.Proj.Paths() {
				paths = append(paths, append(Path{name}, path...))
			}
		} else {
			paths = append(paths, Path{name})
		}
	}
	return paths
}

// HasTypesAnywhere returns true if p contains a type node or an assertion
// node at any depth.
func (p Projection) HasTypesAnywhere() bool {
	return slices.ContainsFunc(p, func(n ProjectionNode) bool {
		return n.Types || n.Assert || n.Proj.HasTypesAnywhere()
	})
}

// An AssertPath is an assertion node of a Projection in the flat form given
// by FlattenAsserts, e.g., for a DAG.  Path leads to the asserted value from
// the root or, if Parent isn't -1, from the value asserted by the AssertPath
// with index Parent.  If HasProjection is false, all of the data of the
// asserted value is needed, and otherwise Projection and TypeProjection are
// as for NewProjectionMaybe.
type AssertPath struct {
	Parent         int    `json:"parent"`
	Path           Path   `json:"path"`
	TypeID         int    `json:"type_id"`
	HasProjection  bool   `json:"has_projection"`
	Projection     []Path `json:"projection"`
	TypeProjection []Path `json:"type_projection"`
}

// FlattenAsserts returns the assertion nodes in p, whose other nodes lead to
// them (see NewProjectionMaybe), as AssertPaths.
func FlattenAsserts(p Projection) []AssertPath {
	var out []AssertPath
	flattenAsserts(p, nil, -1, &out)
	return out
}

func flattenAsserts(p Projection, path Path, parent int, out *[]AssertPath) {
	for _, node := range p {
		if !node.Assert {
			flattenAsserts(node.Proj, append(slices.Clone(path), node.Name), parent, out)
			continue
		}
		i := len(*out)
		*out = append(*out, AssertPath{Parent: parent, Path: path, TypeID: node.TypeID, HasProjection: node.Proj != nil})
		if node.Proj != nil {
			var fields, typeFields []Path
			asserts := node.Proj.split(nil, &fields, &typeFields)
			(*out)[i].Projection = fields
			(*out)[i].TypeProjection = typeFields
			flattenAsserts(asserts, nil, i, out)
		}
	}
}

// split appends the paths of the data and of the types needed by p to fields
// and typeFields and returns the nodes of p that lead to assertion nodes.
func (p Projection) split(path Path, fields, typeFields *[]Path) Projection {
	var out Projection
	for _, node := range p {
		switch {
		case node.Types:
			*typeFields = append(*typeFields, slices.Clone(path))
		case node.Assert:
			out = append(out, node)
		case node.Proj == nil:
			*fields = append(*fields, append(slices.Clone(path), node.Name))
		default:
			if sub := node.Proj.split(append(slices.Clone(path), node.Name), fields, typeFields); sub != nil {
				out = append(out, ProjectionNode{Name: node.Name, Proj: sub})
			}
		}
	}
	return out
}

// UnflattenAsserts returns the Projection for asserts (see FlattenAsserts).
func UnflattenAsserts(asserts []AssertPath) Projection {
	return unflattenAsserts(asserts, -1)
}

func unflattenAsserts(asserts []AssertPath, parent int) Projection {
	var p Projection
	for i, a := range asserts {
		if a.Parent != parent {
			continue
		}
		node := ProjectionNode{Assert: true, TypeID: a.TypeID}
		if a.HasProjection {
			node.Proj = NewProjectionMaybe(true, a.Projection, a.TypeProjection, unflattenAsserts(asserts, i))
		}
		for _, name := range slices.Backward(a.Path) {
			node = ProjectionNode{Name: name, Proj: Projection{node}}
		}
		p = p.insertAsserts(Projection{node})
	}
	return p
}
