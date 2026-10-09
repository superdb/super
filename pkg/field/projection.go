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

func NewProjectionMaybeNone(none bool, paths []Path) Projection {
	if none {
		return NewProjectionNone()
	}
	return NewProjection(paths)
}

func NewProjectionNone() Projection {
	return []ProjectionNode{}
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
