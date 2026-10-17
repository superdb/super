package loader

import (
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
)

type option struct {
	mu     sync.Mutex
	meta   *bsup.Option
	len    uint32
	tags   []uint32
	values shadow
}

func newOption(bctx *bsup.Context, meta *bsup.Option) *option {
	return &option{
		meta: meta,
		len:  meta.Len(bctx),
	}
}

func (o *option) length() uint32 {
	return o.len
}

func (o *option) unmarshal(bctx *bsup.Context, projection field.Projection) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.values == nil {
		o.values = newShadow(bctx, o.meta.Values)
	}
	o.values.unmarshal(bctx, projection)
}

func (o *option) load(loader *FrameLoader) []uint32 {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.tags != nil {
		return o.tags
	}
	tags, err := bsup.ReadUint32s(o.meta.Tags, loader.frame)
	if err != nil {
		panic(err)
	}
	o.tags = tags
	return tags
}

func (o *option) project(loader *FrameLoader, projection field.Projection) vector.Any {
	typ, err := loader.sctx.TranslateType(o.meta.Type)
	if err != nil {
		panic(err)
	}
	optionType := typ.(*super.TypeOption)
	vec := o.values.project(loader, projection)
	if vec.Kind() == vector.KindMissing {
		return vector.NewNone(optionType, vec.Len())
	}
	noneLen := o.len - vec.Len()
	if noneLen == 0 {
		return vector.NewSome(optionType, vec)
	}
	tags := o.load(loader)
	return vector.NewOption(optionType, tags, vec, vector.NewMissing(noneLen))
}
