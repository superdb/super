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
	// none is true if the values are all none, in which case values encodes
	// a none vector.
	none bool
}

var _ Encoder = (*OptionEncoder)(nil)

func NewOptionEncoder(typedefs *TypeDefs, vec *vector.Option) *OptionEncoder {
	var tags []uint32
	values := vec.Any
	if d, ok := values.(*vector.Dynamic); ok {
		//XXX should use RLE
		tags = d.Tags
		values = d.Values[super.OptionSomeTag]
	}
	return &OptionEncoder{
		values: NewEncoder(typedefs, values),
		tags:   NewUint32Encoder(tags),
		count:  vec.Len(),
		none:   values.Kind() == vector.KindNone,
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

func (o *OptionEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.WriteCount(o.count)
	_, tags := o.tags.Segment(w.Offset())
	w.Segment(tags)
	if o.none {
		// The values are all none, which isn't a vector of the option's
		// inner type.
		w.WriteKind(KindNone)
		w.WriteCount(o.count)
		return
	}
	w.Locate(o.values)
}

func (o *OptionEncoder) Stats(path field.Path, fn CollectStatsFunc) {
	o.values.Stats(path, fn)
}
