package loader

import (
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

// Option is an option vector, whose locators are its kind, count, the segment
// of its tags, and the locators of its values, which are a none vector (see
// bsup.KindNone) if the values are all none.
type Option struct {
	Node
	Tags   bsup.Segment
	Values Shadow
	mu     sync.Mutex
	tags   []uint32
}

func (o *Option) load(loader *FrameLoader) []uint32 {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.tags == nil {
		tags, err := bsup.ReadUint32s(o.Tags, loader.frame)
		if err != nil {
			panic(err)
		}
		o.tags = tags
	}
	return o.tags
}

func (o *Option) project(loader *FrameLoader, projection field.Projection) vector.Any {
	typ := loader.vectorType(o.TypeID).(*super.TypeOption)
	vec := project(loader, o.Values, projection)
	if vec.Kind() == vector.KindNone {
		return vector.NewOptionNone(typ, vec.Len())
	}
	noneLen := o.Length - vec.Len()
	if noneLen == 0 {
		return vector.NewOptionSome(typ, vec)
	}
	tags := o.load(loader)
	return vector.NewOption(typ, tags, vec, vector.NewNone(noneLen))
}
