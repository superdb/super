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
type Projection []ProjectionNode

type ProjectionNode struct {
	Name  string
	Proj  Projection
	Types bool
}

func NewProjection(paths []Path) Projection {
	var p Projection
	for _, path := range paths {
		p = p.insertPath(path)
	}
	return p
}

// NewProjectionMaybe returns the Projection for paths where only the types
// are needed for the values at typePaths.
func NewProjectionMaybe(hasProjection bool, paths, typePaths []Path) Projection {
	if !hasProjection {
		return nil
	}
	if len(paths) == 0 && len(typePaths) == 0 {
		return []ProjectionNode{}
	}
	p := NewProjection(paths)
	for _, path := range typePaths {
		p = p.insertTypePath(path)
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
		return !n.Types && n.Name == name
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

// TypesOnly returns true if p consists of just a type node.
func (p Projection) TypesOnly() bool {
	return len(p) == 1 && p[0].Types
}

// NoTypes returns p with each node that needs only types replaced with
// a node that needs all of the data.  It is useful for readers that do not
// support type-only projections.
func (p Projection) NoTypes() Projection {
	if p.HasTypes() {
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
		if node.Types {
			continue
		}
		name := node.Name
		if len(node.Proj) > 0 && !node.Proj.HasTypes() {
			for _, path := range node.Proj.Paths() {
				paths = append(paths, append(Path{name}, path...))
			}
		} else {
			paths = append(paths, Path{name})
		}
	}
	return paths
}
