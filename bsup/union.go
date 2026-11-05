package bsup

import (
	"io"

	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type UnionEncoder struct {
	values    []Encoder
	rleOrTags *Uint32Encoder
	count     uint32
}

var _ Encoder = (*UnionEncoder)(nil)

func NewUnionEncoder(typedefs *TypeDefs, vec *vector.Union) *UnionEncoder {
	var values []Encoder
	for _, val := range vec.Values() {
		values = append(values, NewEncoder(typedefs, val))
	}
	var rleOrTags []uint32
	if len(vec.Typ.Types) == 2 {
		rleOrTags = vec.TagsRLE()
	} else {
		rleOrTags = vec.Tags()
	}
	return &UnionEncoder{
		values:    values,
		rleOrTags: NewUint32Encoder(rleOrTags),
		count:     vec.Len(),
	}
}

func (u *UnionEncoder) Emit(w io.Writer) error {
	if err := u.rleOrTags.Emit(w); err != nil {
		return err
	}
	for _, value := range u.values {
		if err := value.Emit(w); err != nil {
			return err
		}
	}
	return nil
}

func (u *UnionEncoder) Encode(group *errgroup.Group) {
	u.rleOrTags.Encode(group)
	for _, value := range u.values {
		value.Encode(group)
	}
}

func (u *UnionEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindPlain)
	w.WriteCount(u.count)
	_, tags := u.rleOrTags.Segment(w.Offset())
	w.Segment(tags)
	for _, val := range u.values {
		w.Locate(val)
	}
}

func (u *UnionEncoder) Stats(path field.Path, fn CollectStatsFunc) {
	for _, v := range u.values {
		v.Stats(path, fn)
	}
}
