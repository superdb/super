package op

import (
	"container/heap"
	"context"
	"sync"

	"github.com/superdb/super"
	samexpr "github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Merge struct {
	ctx context.Context
	cmp samexpr.CompareFn

	heap    []*mergeParent
	index   []uint32
	once    sync.Once
	parents []*mergeParent
}

func NewMerge(ctx context.Context, parents []vio.Puller, cmp samexpr.CompareFn) *Merge {
	var mergeParents []*mergeParent
	for i, p := range parents {
		mergeParents = append(mergeParents, &mergeParent{
			ctx:      ctx,
			parent:   p,
			resultCh: make(chan result),
			doneCh:   make(chan struct{}),
			tag:      uint32(i),
		})
	}
	return &Merge{
		ctx:     ctx,
		cmp:     cmp,
		parents: mergeParents,
	}
}

func (m *Merge) Pull(done bool) (vector.Any, error) {
	var err error
	m.once.Do(func() {
		for _, p := range m.parents {
			go p.run()
		}
		err = m.start()
	})
	if err != nil {
		return nil, err
	}
	if done || m.Len() == 0 {
		for _, parent := range m.heap {
			select {
			case parent.doneCh <- struct{}{}:
			case <-m.ctx.Done():
				return nil, m.ctx.Err()
			}
		}
		// Restart parents and return EOS.
		return nil, m.start()
	}
	tags := make([]uint32, 0, 2048)
	for {
		tag, endOfVector := m.nextTag()
		tags = append(tags, tag)
		var views []vector.Any
		if endOfVector || len(tags) == cap(tags) {
			views = m.createViews()
		}
		if err := m.updateHeap(); err != nil {
			return nil, err
		}
		if len(views) > 0 {
			return vector.Stitch(vector.NewDynamic(tags, views)), nil
		}
	}
}

func (m *Merge) start() error {
	m.heap = m.heap[:0]
	for _, parent := range m.parents {
		ok, err := parent.replenish()
		if err != nil {
			return err
		}
		if ok {
			heap.Push(m, parent)
		}
	}
	return nil
}

func (m *Merge) nextTag() (tag uint32, endOfVector bool) {
	min := m.heap[0]
	min.off++
	return min.tag, min.off >= min.vec.Len()
}

func (m *Merge) updateHeap() error {
	min := m.heap[0]
	if min.off < min.vec.Len() {
		min.updateVal()
		heap.Fix(m, 0)
		return nil
	}
	ok, err := min.replenish()
	if err != nil {
		return err
	}
	if !ok {
		heap.Pop(m)
	}
	heap.Fix(m, 0)
	return nil
}

func (m *Merge) createViews() []vector.Any {
	views := make([]vector.Any, len(m.parents))
	for i, p := range m.parents {
		if p.vec == nil || p.off == p.lastOff {
			continue
		}
		if int(p.off) >= len(m.index) {
			m.index = make([]uint32, p.off)
			for i := range m.index {
				m.index[i] = uint32(i)
			}
		}
		index := m.index[p.lastOff:p.off]
		views[i] = vector.Pick(p.vec, index)
		p.lastOff = p.off
	}
	return views
}

func (m *Merge) Len() int           { return len(m.heap) }
func (m *Merge) Less(i, j int) bool { return m.cmp(m.heap[i].val, m.heap[j].val) < 0 }
func (m *Merge) Swap(i, j int)      { m.heap[i], m.heap[j] = m.heap[j], m.heap[i] }
func (m *Merge) Push(x any)         { m.heap = append(m.heap, x.(*mergeParent)) }

func (m *Merge) Pop() any {
	x := m.heap[m.Len()-1]
	m.heap = m.heap[:m.Len()-1]
	return x
}

type mergeParent struct {
	ctx      context.Context
	parent   vio.Puller
	resultCh chan result
	doneCh   chan struct{}
	tag      uint32

	vec     vector.Any
	off     uint32
	lastOff uint32
	sb      scode.Builder
	val     super.Value
}

func (m *mergeParent) run() {
	for {
		vec, err := m.parent.Pull(false)
	Select:
		select {
		case m.resultCh <- result{vec, err}:
		case <-m.doneCh:
			vec, err = m.parent.Pull(true)
			if err != nil {
				// Send err downstream.
				goto Select
			}
		case <-m.ctx.Done():
			return
		}
	}
}

// replenish tries to receive the next vector.  It returns false when EOS
// is encountered and its goroutine will then block until resumed or
// canceled.
func (m *mergeParent) replenish() (bool, error) {
	select {
	case r := <-m.resultCh:
		if r.vector == nil || r.err != nil {
			m.vec = nil
			return false, r.err
		}
		m.vec = r.vector
		m.off = 0
		m.lastOff = 0
		m.updateVal()
		return true, nil
	case <-m.ctx.Done():
		return false, m.ctx.Err()
	}
}

func (m *mergeParent) updateVal() {
	m.val = vector.ValueAt(&m.sb, m.vec, m.off)
}
