package vbuild

import (
	"fmt"
	"runtime/debug"

	"github.com/superdb/super"
	"github.com/superdb/super/vector"
)

type DynamicBuilder struct {
	tags   []uint32
	values []Builder
	which  map[super.Type]uint32
}

func NewDynamicBuilder() *DynamicBuilder {
	return &DynamicBuilder{which: make(map[super.Type]uint32)}
}

func (d *DynamicBuilder) Write(vec vector.Any) {
	fmt.Println("D", vector.Format(vec))
	if dynamic, ok := vec.(*vector.Dynamic); ok {
		debug.PrintStack()
		tagMap := make([]uint32, len(dynamic.Values))
		for i, vec := range dynamic.Values {
			if vec != nil {
				if _, ok := vec.(*vector.Dynamic); ok {
					fmt.Println("=== D ===")
					debug.PrintStack()
				}
				tagMap[i] = d.write(vec)
			}
		}
		for i := range vec.Len() {
			d.tags = append(d.tags, tagMap[dynamic.Tags[i]])
		}
	} else {
		tag := d.write(vec)
		for range vec.Len() {
			d.tags = append(d.tags, tag)
		}
	}
}

func (d *DynamicBuilder) write(vec vector.Any) uint32 {
	typ := vec.Type()
	i, ok := d.which[typ]
	if !ok {
		i = uint32(len(d.values))
		d.which[typ] = i
		d.values = append(d.values, New(typ))
	}
	if vec.Len() != 0 {
		d.values[i].Write(vec)
	}
	return i
}

func (d *DynamicBuilder) Len() uint32 {
	return uint32(len(d.tags))
}

func (d *DynamicBuilder) Build() vector.Any {
	out := d.BuildDynamic()
	if len(out.Values) == 1 {
		return out.Values[0]
	}
	return out
}

func (d *DynamicBuilder) BuildDynamic() *vector.Dynamic {
	var vecs []vector.Any
	for _, b := range d.values {
		vecs = append(vecs, b.Build())
	}
	return vector.NewDynamic(d.tags, vecs)
}
