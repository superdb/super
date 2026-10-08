package meta

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/db"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/order"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sbuf"
	"golang.org/x/sync/errgroup"
)

// Lister enumerates all the data.Objects in a scan.  A Slicer downstream may
// optionally organize objects into non-overlapping partitions for merge on read.
// The optimizer may decide when partitions are necessary based on the order
// sensitivity of the downstream flowgraph.
type Lister struct {
	ctx       context.Context
	pool      *db.Pool
	pruner    *pruner
	group     *errgroup.Group
	marshaler *super.Marshaler
	mu        sync.Mutex
	objects   []*data.Object
	err       error
}

var _ sbuf.Puller = (*Lister)(nil)

func NewSortedLister(ctx context.Context, sctx *super.Context, pool *db.Pool, commit uuid.UUID, pruner expr.Evaluator) (*Lister, error) {
	snap, err := pool.Snapshot(ctx, commit)
	if err != nil {
		return nil, err
	}
	objects := snap.Select(nil, pool.SortKeys.Primary().Order)
	return NewSortedListerFromObjects(ctx, sctx, pool, objects, pruner), nil
}

func NewSortedListerFromObjects(ctx context.Context, sctx *super.Context, pool *db.Pool, objects []*data.Object, pruner expr.Evaluator) *Lister {
	sortObjects(objects, pool.SortKeys.Primary().Order)
	m := super.NewMarshaler(sctx)
	m.Decorate(super.StylePackage)
	l := &Lister{
		ctx:       ctx,
		pool:      pool,
		objects:   objects,
		group:     &errgroup.Group{},
		marshaler: m,
	}
	if pruner != nil {
		l.pruner = newPruner(pruner)
	}
	return l
}

func (l *Lister) Pull(done bool) (sbuf.Batch, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return nil, l.err
	}
	for len(l.objects) != 0 {
		o := l.objects[0]
		l.objects = l.objects[1:]
		val, err := l.marshaler.Marshal(o)
		if err != nil {
			l.err = err
			return nil, err
		}
		if !l.pruner.prune(val) {
			return sbuf.NewArray([]super.Value{val}), nil
		}
	}
	return nil, nil
}

func sortObjects(objects []*data.Object, o order.Which) {
	cmp := expr.NewValueCompareFn(o, o.NullsMax(true))
	lessFunc := func(a, b *data.Object) bool {
		aFrom, aTo, bFrom, bTo := a.Min, a.Max, b.Min, b.Max
		if o == order.Desc {
			aFrom, aTo, bFrom, bTo = aTo, aFrom, bTo, bFrom
		}
		if cmp(aFrom, bFrom) < 0 {
			return true
		}
		if !bytes.Equal(aFrom.Bytes(), bFrom.Bytes()) {
			return false
		}
		if bytes.Equal(aTo.Bytes(), bTo.Bytes()) {
			// If the pool keys are equal for both the first and last values
			// in the object, we return false here so that the stable sort preserves
			// the commit order of the objects in the log. XXX we might want to
			// simply sort by commit timestamp for a more robust API that does not
			// presume commit-order in the object snapshot.
			return false
		}
		return cmp(aTo, bTo) < 0
	}
	sort.SliceStable(objects, func(i, j int) bool {
		return lessFunc(objects[i], objects[j])
	})
}
