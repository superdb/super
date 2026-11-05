package bsup

import (
	"io"

	"github.com/superdb/super/pkg/field"
	"golang.org/x/sync/errgroup"
)

type DictEncoder struct {
	values Encoder
	counts *Uint32Encoder
	index  []byte
}

func (d *DictEncoder) Encode(group *errgroup.Group) {
	d.values.Encode(group)
	d.counts.Encode(group)
}

// Locate encodes the locators of a dict, which are its values, the segment of
// its counts, and the segment of its index, whose length is the dict's length.
func (d *DictEncoder) Locate(w *LocatorWriter) {
	w.WriteKind(KindDict)
	w.Locate(d.values)
	_, counts := d.counts.Segment(w.Offset())
	w.Segment(counts)
	len := uint64(len(d.index))
	w.Segment(Segment{
		Offset:    w.Offset(),
		Length:    len,
		MemLength: len,
	})
}

func (d *DictEncoder) Stats(path field.Path, fn CollectStatsFunc) {
	d.values.Stats(path, fn)
}

func (d *DictEncoder) Emit(w io.Writer) error {
	if err := d.values.Emit(w); err != nil {
		return err
	}
	if err := d.counts.Emit(w); err != nil {
		return err
	}
	var err error
	if len(d.index) > 0 {
		_, err = w.Write(d.index)
	}
	return err
}
