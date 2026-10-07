package op

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/superdb/super/runtime"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// Catcher wraps an Puller that recovers panics and turns them into errors.
type Catcher struct {
	parent vio.Puller
}

func NewCatcher(parent vio.Puller) *Catcher {
	return &Catcher{parent}
}

func (c *Catcher) Pull(done bool) (vec vector.Any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %+v\n%s\n", r, debug.Stack())
		}
	}()
	return c.parent.Pull(done)
}

// Mux implements the muxing of a set of parallel paths at the output of
// a flowgraph.  It also implements the double-EOS algorithm with proc.Latch
// to detect the end of each parallel stream.  Its output protocol is a single EOS
// when all of the upstream legs are done at which time it cancels the flowgraoh.
// Each  batch returned by the mux is wrapped in a Batch, which can be unwrappd
// with Unwrap to extract the integer index of the output (in left-to-right
// DFS traversal order of the flowgraph).  This proc requires more than one
// parent; use proc.Latcher for a single-output flowgraph.
type Mux struct {
	rctx     *runtime.Context
	once     sync.Once
	ch       <-chan muxresult
	parents  []*puller
	nparents int
	debugger *debugger
	eosCh    chan<- struct{}
}

type DebugChans struct {
	Debug []chan vector.Any
	EOS   chan struct{}
}

func NewDebugChans() *DebugChans {
	return &DebugChans{EOS: make(chan struct{})}
}

func (d *DebugChans) Next() chan vector.Any {
	ch := make(chan vector.Any)
	d.Debug = append(d.Debug, ch)
	return ch
}

type muxresult struct {
	vec   vector.Any
	label string
	err   error
}

func (m muxresult) vector() *vector.Labeled {
	return &vector.Labeled{Any: m.vec, Label: m.label}
}

type puller struct {
	vio.Puller
	ch    chan<- muxresult
	label string
}

func (p *puller) run(ctx context.Context) {
	for {
		vec, err := p.Pull(false)
		select {
		case p.ch <- muxresult{vec, p.label, err}:
			if vec == nil || err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func NewMux(rctx *runtime.Context, parents map[string]vio.Puller, chans *DebugChans) *Mux {
	if len(parents)+len(chans.Debug) <= 1 {
		panic("NewMux must be called with two or more parents")
	}
	ch := make(chan muxresult)
	pullers := make([]*puller, 0, len(parents))
	for label, parent := range parents {
		pullers = append(pullers, &puller{NewCatcher(parent), ch, label})
	}
	var debugger *debugger
	if len(chans.Debug) != 0 {
		debugger = newDebugger(chans)
	}
	return &Mux{
		rctx:     rctx,
		ch:       ch,
		parents:  pullers,
		nparents: len(parents),
		debugger: debugger,
		eosCh:    chans.EOS,
	}
}

// Pull implements the merge logic for returning data from the upstreams.
func (m *Mux) Pull(bool) (vector.Any, error) {
	m.once.Do(func() {
		for _, puller := range m.parents {
			go puller.run(m.rctx.Context)
		}
		if m.debugger != nil {
			m.debugger.run()
		}
	})
	for m.nparents != 0 || m.debugger.active() {
		select {
		case res := <-m.ch:
			if res.err != nil {
				m.rctx.Cancel()
				return nil, res.err
			}
			if res.vec == nil {
				m.nparents--
				if m.nparents == 0 {
					close(m.eosCh)
				}
			}
			return res.vector(), nil
		case res := <-m.debugger.channel():
			m.debugger.check(res)
			if res.err != nil {
				m.rctx.Cancel()
				return nil, res.err
			}
			if res.vec != nil {
				return res.vector(), nil
			}
		case <-m.rctx.Context.Done():
			return nil, m.rctx.Context.Err()
		}
	}
	m.rctx.Cancel()
	return nil, nil
}

type Single struct {
	vio.Puller
	label string
	eos   bool
}

func NewSingle(label string, parent vio.Puller) *Single {
	return &Single{Puller: parent, label: label}
}

func (s *Single) Pull(bool) (vector.Any, error) {
	if s.eos {
		return nil, nil
	}
	vec, err := s.Puller.Pull(false)
	if vec == nil {
		s.eos = true
	}
	return &vector.Labeled{Any: vec, Label: s.label}, err
}

func Unlabel(vec vector.Any) (vector.Any, string) {
	if vec, ok := vec.(*vector.Labeled); ok {
		return vec.Any, vec.Label
	}
	return vec, ""
}

type debugger struct {
	threads  []dthread
	resultCh chan muxresult
	nrun     int
}

func newDebugger(chans *DebugChans) *debugger {
	var threads []dthread
	resultCh := make(chan muxresult)
	for _, ch := range chans.Debug {
		threads = append(threads, dthread{
			vecCh:    ch,
			resultCh: resultCh,
		})
	}
	return &debugger{
		threads:  threads,
		resultCh: resultCh,
		nrun:     len(threads),
	}
}

func (d *debugger) active() bool {
	return d != nil && d.nrun != 0
}

func (d *debugger) run() {
	for _, t := range d.threads {
		go t.run()
	}
}

func (d *debugger) check(r muxresult) {
	if r.vec == nil {
		d.nrun--
	}
}

func (d *debugger) channel() <-chan muxresult {
	if d == nil {
		return nil
	}
	return d.resultCh
}

type dthread struct {
	vecCh    <-chan vector.Any
	resultCh chan<- muxresult
}

func (d *dthread) run() {
	for {
		vec := <-d.vecCh
		d.resultCh <- muxresult{vec, "debug", nil}
		if vec == nil {
			return
		}
	}
}
