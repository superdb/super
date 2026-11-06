package vector

import (
	"sync/atomic"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
)

// Dynamic is an ordered sequence of values taken from one or more
// hetereogenously-typed vectors.
type Dynamic struct {
	Tags   []uint32
	Values []Any

	// Tag maps are used to map slots between a Dynamic and its values
	// vectors in both the forward and reverse directions.  We need this
	// because vectors are stored in a dense format where different types
	// hold only the values needed for that type.  If we stored vectors in a
	// sparse format, the overhead would increase substantially for
	// heterogeneously-typed data.
	forwardTagMap atomic.Pointer[[]uint32]
	reverseTagMap atomic.Pointer[[][]uint32]
}

func NewDynamic(tags []uint32, values []Any) *Dynamic {
	return &Dynamic{Tags: tags, Values: values}
}

func (*Dynamic) Kind() Kind {
	return KindInvalid
}

func (*Dynamic) Type() super.Type {
	panic("can't call Type() on a vector.Dynamic")
}

func (d *Dynamic) TypeOf(slot uint32) super.Type {
	return d.Values[d.Tags[slot]].Type()
}

func (d *Dynamic) Len() uint32 {
	if d.Tags != nil {
		return uint32(len(d.Tags))
	}
	var length uint32
	for _, val := range d.Values {
		length += val.Len()
	}
	return length
}

func (d *Dynamic) Serialize(b *scode.Builder, slot uint32) {
	d.Values[d.Tags[slot]].Serialize(b, d.ForwardTagMap()[slot])
}

func (d *Dynamic) ForwardTagMap() []uint32 {
	if t := d.forwardTagMap.Load(); t != nil {
		return *t
	}
	if t := d.newForwardTagMap(); d.forwardTagMap.CompareAndSwap(nil, &t) {
		return t
	}
	return *d.forwardTagMap.Load()
}

func (d *Dynamic) newForwardTagMap() []uint32 {
	counts := make([]uint32, len(d.Values))
	forward := make([]uint32, len(d.Tags))
	for slot, tag := range d.Tags {
		forward[slot] = counts[tag]
		counts[tag]++
	}
	return forward
}

func (d *Dynamic) ReverseTagMap() [][]uint32 {
	if t := d.reverseTagMap.Load(); t != nil {
		return *t
	}
	if t := d.newReverseTagMap(); d.reverseTagMap.CompareAndSwap(nil, &t) {
		return t
	}
	return *d.reverseTagMap.Load()
}

func (d *Dynamic) newReverseTagMap() [][]uint32 {
	reverse := make([][]uint32, len(d.Values))
	space := make([]uint32, len(d.Tags))
	var off uint32
	for tag, vec := range d.Values {
		var n uint32
		if vec != nil {
			n = vec.Len()
		}
		reverse[tag] = space[off : off+n]
		off += n
	}
	counts := make([]uint32, len(d.Values))
	for slot, tag := range d.Tags {
		childSlot := counts[tag]
		reverse[tag][childSlot] = uint32(slot)
		counts[tag]++
	}
	return reverse
}

func IsDynamic(vec Any) bool {
	_, ok := vec.(*Dynamic)
	return ok
}
