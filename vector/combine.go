package vector

import "github.com/superdb/super"

func Combine(base Any, index []uint32, vec Any) Any {
	c := NewCombiner(base)
	c.Add(index, vec)
	return c.Result()
}

type Combiner struct {
	base    Any
	vecs    []Any
	indexes [][]uint32
}

func NewCombiner(base Any) *Combiner {
	return &Combiner{base: base}
}

func (c *Combiner) WrappedError(sctx *super.Context, index []uint32, msg string, inner Any) {
	c.Add(index, NewWrappedError(sctx, msg, Pick(inner, index)))
}

func (c *Combiner) Add(index []uint32, vec Any) {
	if len(index) == 0 {
		return
	}
	c.vecs = append(c.vecs, vec)
	c.indexes = append(c.indexes, index)
}

func (c *Combiner) Result() Any {
	if len(c.vecs) == 0 {
		return c.base
	}
	var baseVecs []Any
	var baseTags []uint32
	if dynamic, ok := c.base.(*Dynamic); ok {
		baseVecs = dynamic.Values
		baseTags = dynamic.Tags
	} else {
		baseVecs = []Any{c.base}
		baseTags = make([]uint32, c.base.Len())
	}
	size := c.base.Len()
	for _, vec := range c.vecs {
		size += vec.Len()
	}
	tags := make([]uint32, int(size))
	n := uint32(len(baseVecs))
	for i := range size {
		var matched bool
		for j, index := range c.indexes {
			if len(index) > 0 && i == index[0] {
				tags[i] = n + uint32(j)
				c.indexes[j] = index[1:]
				matched = true
				break
			}
		}
		if !matched {
			tags[i] = baseTags[0]
			baseTags = baseTags[1:]
		}
	}
	// Flatten any Dynamics among c.vecs so the result never nests a Dynamic.
	return stitch(tags, append(baseVecs, c.vecs...))
}
