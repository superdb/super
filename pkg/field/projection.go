package field

import "slices"

type Projection []ProjectionNode

type ProjectionNode struct {
	Name string
	Proj Projection
}

func NewProjection(paths []Path) Projection {
	var p Projection
	for _, path := range paths {
		p = p.insertPath(path)
	}
	return p
}

func NewProjectionMaybe(hasProjection bool, paths []Path) Projection {
	if hasProjection && len(paths) == 0 {
		return []ProjectionNode{}
	}
	return NewProjection(paths)
}

func (p Projection) insertPath(path Path) Projection {
	if len(path) == 0 {
		return nil
	}
	i := slices.IndexFunc(p, func(n ProjectionNode) bool {
		return n.Name == path[0]
	})
	if i < 0 {
		i = len(p)
		p = append(p, ProjectionNode{Name: path[0]})
	}
	p[i].Proj = p[i].Proj.insertPath(path[1:])
	return p
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
		pmap[node.Name] = node.Proj
	}
	for _, qnode := range q {
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

func (p Projection) Paths() []Path {
	var paths []Path
	for _, node := range p {
		name := node.Name
		if len(node.Proj) > 0 {
			for _, path := range node.Proj.Paths() {
				paths = append(paths, append(Path{name}, path...))
			}
		} else {
			paths = append(paths, Path{name})
		}
	}
	return paths
}
