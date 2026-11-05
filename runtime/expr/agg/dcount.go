package agg

import (
	"fmt"

	"github.com/axiomhq/hyperloglog"
	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
)

// dcount uses hyperloglog to approximate the count of unique values for
// a field.
type dcount struct {
	sketches []*hyperloglog.Sketch
}

func newDCount() *dcount {
	return &dcount{}
}

func (d *dcount) Resize(n uint32) {
	for range n - uint32(len(d.sketches)) {
		d.sketches = append(d.sketches, hyperloglog.New())
	}
}

func (d *dcount) Consume(groups []uint32, vec vector.Any) {
	if vec.Kind() == vector.KindNull {
		return
	}
	// append type id to vals so we get a unique count where the bytes are same
	// but the super.Type is different.
	scratch := super.AppendInt(nil, int64(vec.Type().ID()))
	n := len(scratch)
	var b scode.Builder
	for slot, g := range orZeros(groups, vec.Len()) {
		b.Truncate()
		vec.Serialize(&b, uint32(slot))
		if vb := b.Bytes().Body(); vb != nil {
			scratch = append(scratch[:n], vb...)
			d.sketches[g].Insert(scratch)
		}
	}
}

func (d *dcount) Result(*super.Context) vector.Any {
	counts := make([]int64, len(d.sketches))
	for g, sketch := range d.sketches {
		counts[g] = int64(sketch.Estimate())
	}
	return vector.NewInt(super.TypeInt64, counts)
}

func (d *dcount) ConsumeAsPartial(groups []uint32, partial vector.Any) {
	if partial.Type() != super.TypeBytes {
		panic("dcount: invalid partial")
	}
	for slot, g := range orZeros(groups, partial.Len()) {
		var s hyperloglog.Sketch
		if err := s.UnmarshalBinary(vector.BytesValue(partial, uint32(slot))); err != nil {
			panic(fmt.Errorf("dcount: unmarshaling partial: %w", err))
		}
		d.sketches[g].Merge(&s)
	}
}

func (d *dcount) ResultAsPartial(sctx *super.Context) vector.Any {
	table := vector.NewBytesTableEmpty(uint32(len(d.sketches)))
	for _, sketch := range d.sketches {
		b, err := sketch.MarshalBinary()
		if err != nil {
			panic(fmt.Errorf("dcount: marshaling partial: %w", err))
		}
		table.Append(b)
	}
	return vector.NewBytes(table)
}
