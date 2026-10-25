package dbmanage

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/db/api"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/db/tables"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/order"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/runtime/sam/expr/extent"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
)

// scan reads the table's data objects from it and sends runs of two or more
// object IDs to compact on runCh.  The strategy depends on whether the table
// is sorted; see scanSorted and scanUnsorted.
func scan(ctx context.Context, it *objectIterator, config *tables.Config, runCh chan<- []uuid.UUID) error {
	send := func(objects []*data.Object) error {
		// Compacting a lone object would just rewrite it.
		if len(objects) < 2 {
			return nil
		}
		select {
		case runCh <- objectIDs(objects):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if _, ok := config.SortKeys.Primary(); ok {
		return scanSorted(it, config.ObjectCap, send)
	}
	return scanUnsorted(it, config.ObjectCap, send)
}

// scanSorted builds runs from objects in key order.  Objects whose key
// ranges overlap are always compacted together, regardless of objectCap,
// since a scan of the table must otherwise merge them.  Consecutive objects
// that don't overlap are also compacted when their combined count fits
// within objectCap.
func scanSorted(it *objectIterator, objectCap uint64, send func([]*data.Object) error) error {
	run := newRunBuilder()
	for {
		o, err := it.next()
		if err != nil {
			return err
		}
		if o == nil {
			return send(run.objects)
		}
		if run.overlaps(o.Min, o.Max) || run.len+o.Count <= objectCap {
			run.add(o)
			continue
		}
		if err := send(run.objects); err != nil {
			return err
		}
		run.reset()
		run.add(o)
	}
}

// smallObjectFraction determines which objects scanUnsorted considers for
// compaction: those holding fewer than objectCap/smallObjectFraction values.
const smallObjectFraction = 2

// scanUnsorted compacts small objects in a table without a sort key, where
// key overlap has no meaning.  Objects smaller than objectCap/
// smallObjectFraction are packed, smallest first, into runs whose combined
// count fits within objectCap.  Larger objects are left alone.  Each
// compaction reduces the number of objects, so repeated passes converge.
func scanUnsorted(it *objectIterator, objectCap uint64, send func([]*data.Object) error) error {
	threshold := objectCap / smallObjectFraction
	var small []*data.Object
	for {
		o, err := it.next()
		if err != nil {
			return err
		}
		if o == nil {
			break
		}
		if o.Count < threshold {
			small = append(small, o)
		}
	}
	slices.SortStableFunc(small, func(a, b *data.Object) int {
		return cmp.Compare(a.Count, b.Count)
	})
	var run []*data.Object
	var n uint64
	for _, o := range small {
		if len(run) > 0 && n+o.Count > objectCap {
			if err := send(run); err != nil {
				return err
			}
			run, n = nil, 0
		}
		run = append(run, o)
		n += o.Count
	}
	return send(run)
}

// iteratorQuery lists a branch's data objects in order of their minimum key.
// It sorts on the defused value but leaves min and max as type any so they
// unmarshal into data.Object.
const iteratorQuery = `
from %q@%q:objects
| sort defuse(min)
`

type objectIterator struct {
	reader      sio.Reader
	puller      sbuf.Puller
	unmarshaler *super.Unmarshaler
}

func newObjectIterator(ctx context.Context, db api.Interface, head *dbid.Committish) (*objectIterator, error) {
	query := fmt.Sprintf(iteratorQuery, head.Table, head.Branch)
	q, err := db.Query(ctx, srcfiles.Plain(query))
	if err != nil {
		return nil, err
	}
	puller := sbuf.NewMaterializer(q)
	u := super.NewUnmarshaler()
	u.SetContext(super.NewContext())
	return &objectIterator{
		reader:      sbuf.PullerReader(puller),
		puller:      puller,
		unmarshaler: u,
	}, nil
}

func (r *objectIterator) next() (*data.Object, error) {
	val, err := r.reader.Read()
	if val == nil || err != nil {
		return nil, err
	}
	o := data.Object{
		Min: super.None,
		Max: super.None,
	}
	if err := r.unmarshaler.Unmarshal(*val, &o); err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *objectIterator) close() error {
	_, err := r.puller.Pull(true)
	return err
}

type runBuilder struct {
	span    extent.Span
	cmp     expr.CompareFn
	objects []*data.Object
	len     uint64
}

func newRunBuilder() *runBuilder {
	return &runBuilder{cmp: expr.NewValueCompareFn(order.Asc, order.NullsLast)}
}

func (r *runBuilder) overlaps(first, last super.Value) bool {
	if r.span == nil {
		return false
	}
	return r.span.Overlaps(first, last)
}

func (r *runBuilder) add(o *data.Object) {
	r.objects = append(r.objects, o)
	r.len += o.Count
	if r.span == nil {
		r.span = extent.NewGeneric(o.Min, o.Max, r.cmp)
		return
	}
	r.span.Extend(o.Min)
	r.span.Extend(o.Max)
}

func objectIDs(objects []*data.Object) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(objects))
	for _, o := range objects {
		ids = append(ids, o.ID)
	}
	return ids
}

func (r *runBuilder) reset() {
	r.span = nil
	r.objects = r.objects[:0]
	r.len = 0
}
