package op

import (
	"errors"
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/order"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// Slicer implements an op that pulls data objects and organizes
// them into overlapping object Slices forming a sequence of
// non-overlapping Partitions.
type Slicer struct {
	sctx        *super.Context
	parent      vio.Puller
	marshaler   *super.Marshaler
	unmarshaler *super.Unmarshaler
	objects     []*data.Object
	cmp         expr.CompareFn
	min         *super.Value
	max         *super.Value
	mu          sync.Mutex
}

func NewSlicer(sctx *super.Context, parent vio.Puller) *Slicer {
	m := super.NewMarshaler(sctx)
	m.Decorate(super.StylePackage)
	return &Slicer{
		sctx:        sctx,
		parent:      parent,
		marshaler:   m,
		unmarshaler: super.NewUnmarshaler(),
		//XXX check that nulls position is consistent for both dirs in database ops
		cmp: expr.NewValueCompareFn(order.Asc, order.NullsLast),
	}
}

func (s *Slicer) Pull(done bool) (vector.Any, error) {
	//XXX for now we use a mutex because multiple downstream trunks can call
	// Pull concurrently here.  We should change this to use a fork.  But for now,
	// this does not seem like a performance critical issue because the bottleneck
	// will be each trunk and the lister parent should run fast in comparison.
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		vec, err := s.parent.Pull(done)
		if err != nil {
			return nil, err
		}
		if vec == nil {
			return s.nextPartition()
		}
		if vec.Len() != 1 {
			// We currently support only one object per batch.
			return nil, errors.New("system error: Slicer encountered multi-valued batch")
		}
		val := vector.ValueAt(nil, vec, 0)
		var object data.Object
		if err := s.unmarshaler.Unmarshal(val, &object); err != nil {
			return nil, err
		}
		if batch, err := s.stash(&object); batch != nil || err != nil {
			return batch, err
		}
	}
}

// nextPartition takes collected up slices and forms a partition returning
// a vector containing a single value comprising the serialized partition.
func (s *Slicer) nextPartition() (vector.Any, error) {
	if len(s.objects) == 0 {
		return nil, nil
	}
	//XXX let's keep this as we go!... need to reorder stuff in stash() to make this work
	min := s.objects[0].Min
	max := s.objects[0].Max
	for _, o := range s.objects[1:] {
		if s.cmp(o.Min, min) < 0 {
			min = o.Min
		}
		if s.cmp(o.Max, max) > 0 {
			max = o.Max
		}
	}
	val, err := s.marshaler.Marshal(&data.Partition{
		Min:     min,
		Max:     max,
		Objects: s.objects,
	})
	s.objects = s.objects[:0]
	if err != nil {
		return nil, err
	}
	return sbuf.Dematerialize(s.sctx, val), nil
}

func (s *Slicer) stash(o *data.Object) (vector.Any, error) {
	var vec vector.Any
	if len(s.objects) > 0 {
		// We collect all the subsequent objects that overlap with any object in the
		// accumulated set so far.  Since first times are non-decreasing this is
		// guaranteed to generate partitions that are non-decreasing and non-overlapping.
		if s.cmp(o.Max, *s.min) < 0 || s.cmp(o.Min, *s.max) > 0 {
			var err error
			vec, err = s.nextPartition()
			if err != nil {
				return nil, err
			}
			s.min = nil
			s.max = nil
		}
	}
	s.objects = append(s.objects, o)
	if s.min == nil || s.cmp(*s.min, o.Min) > 0 {
		s.min = o.Min.Copy().Ptr()
	}
	if s.max == nil || s.cmp(*s.max, o.Max) < 0 {
		s.max = o.Max.Copy().Ptr()
	}
	return vec, nil
}
