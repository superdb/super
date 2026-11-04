package bsup

import (
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type OptionEncoder struct {
	values Encoder
	tags   *Uint32Encoder
	count  uint32
	typ    super.Type
}

var _ Encoder = (*UnionEncoder)(nil)

func NewOptionEncoder(bctx *Context, vec *vector.Option) *OptionEncoder {
	var tags []uint32
	values := vec.Any
	if d, ok := values.(*vector.Dynamic); ok {
		//XXX should use RLE
		tags = d.Tags
		values = d.Values[super.OptionSomeTag]
	}
	return &OptionEncoder{
		values: NewEncoder(bctx, values),
		tags:   NewUint32Encoder(tags),
		count:  vec.Len(),
		typ:    vec.Type(),
	}
}

func (o *OptionEncoder) Emit(w io.Writer) error {
	if err := o.tags.Emit(w); err != nil {
		return err
	}
	return o.values.Emit(w)
}

func (o *OptionEncoder) Encode(group *errgroup.Group) {
	o.tags.Encode(group)
	o.values.Encode(group)
}

func (o *OptionEncoder) Metadata(bctx *Context, off uint64) (uint64, ID) {
	off, tags := o.tags.Segment(off)
	off, id := o.values.Metadata(bctx, off)
	return off, bctx.enter(&Option{
		bctx.TypeDefs().LookupType(o.typ),
		o.count,
		tags,
		id,
	})
}

func (o *OptionEncoder) Stats(path field.Path, fn CollectStatsFunc) {
	o.values.Stats(path, fn)
}
