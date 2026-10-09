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

func newOption(cctx *bsup.Context, meta *bsup.Option) *option {
	return &option{
		meta: meta,
		len:  meta.Len(cctx),
	}
}

func (o *option) length() uint32 {
	return o.len
}

func (o *option) unmarshal(cctx *bsup.Context, projection field.Projection) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.values == nil {
		o.values = newShadow(cctx, o.meta.Values)
	}
	o.values.unmarshal(cctx, projection)
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
	if vec.Kind() == vector.KindNone {
		return vector.NewOption(optionType, vec)
	}
	nones := o.len - vec.Len()
	if nones == 0 {
		return vector.NewOption(optionType, vec)
	}
	tags := o.load(loader)
	return vector.NewOptionBoth(optionType, tags, vec, vector.NewNone(nones))
}
