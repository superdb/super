package bsup

import (
	"io"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/vector"
	"golang.org/x/sync/errgroup"
)

type DynamicEncoder struct {
	typedefs *TypeDefs
	tags     *Uint32Encoder
	types    []super.Type
	values   []Encoder
	len      uint32
}

func NewDynamicEncoder(vec *vector.Dynamic) *DynamicEncoder {
	typedefs := NewTypeDefs()
	values := make([]Encoder, len(vec.Values))
	types := make([]super.Type, len(vec.Values))
	for i, val := range vec.Values {
		values[i] = NewEncoder(typedefs, val)
		types[i] = val.Type()
	}
	return &DynamicEncoder{
		typedefs: typedefs,
		tags:     NewUint32Encoder(vec.Tags),
		types:    types,
		values:   values,
		len:      vec.Len(),
	}
}

// Encode encodes the vectors and returns the frame's vector typedefs table and
// locator section and the size of its data section.  The locator section is
// the number of root vectors and the type ID of each in the vector typedefs
// table, followed by the segment of the tags if there's more than one root
// vector (i.e., a dynamic), then the locators of each root vector.
func (d *DynamicEncoder) Encode() ([]byte, []byte, uint64, error) {
	var group errgroup.Group
	if len(d.values) > 1 {
		d.tags.Encode(&group)
	}
	for _, val := range d.values {
		val.Encode(&group)
	}
	if err := group.Wait(); err != nil {
		return nil, nil, 0, err
	}
	types := super.NewTypeDefsEncoder()
	var w LocatorWriter
	w.WriteCount(uint32(len(d.values)))
	for _, typ := range d.types {
		w.WriteUvarint(uint64(types.LookupType(typ)))
	}
	if len(d.values) > 1 {
		_, tags := d.tags.Segment(w.Offset())
		w.Segment(tags)
	}
	for _, val := range d.values {
		w.Locate(val)
	}
	if err := w.Err(); err != nil {
		return nil, nil, 0, err
	}
	return types.Bytes(), w.Bytes(), w.Offset(), nil
}

func (d *DynamicEncoder) Stats() []ColumnStats {
	var paths field.List
	var stats []ColumnStats
	cmp := expr.NewValueCompareFn(order.Asc, order.NullsLast)
	for _, v := range d.values {
		v.Stats(nil, func(path field.Path, min, max super.Value) {
			i := paths.Index(path)
			if i < 0 {
				p := slices.Clone(path)
				paths = append(paths, p)
				stats = append(stats, ColumnStats{p, min, max})
				return
			}
			stat := &stats[i]
			// XXX strings values should get truncated if min/max is greater than
			// a certain length... Perhaps 128 bytes?
			if cmp(min, stat.Min) < 0 {
				stat.Min = min
			}
			if cmp(max, stat.Max) > 0 {
				stat.Max = max
			}
		})
	}
	// Sort by path so we don't get panics when merging this into
	// a single record.
	slices.SortFunc(stats, func(a, b ColumnStats) int {
		return a.Path.Compare(b.Path)
	})
	return stats
}

func (d *DynamicEncoder) Emit(w io.Writer) error {
	if len(d.values) > 1 {
		if err := d.tags.Emit(w); err != nil {
			return err
		}
	}
	for _, value := range d.values {
		if err := value.Emit(w); err != nil {
			return err
		}
	}
	return nil
}
