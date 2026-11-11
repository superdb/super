package op

import (
	"context"
	"fmt"
	"runtime/debug"
	"slices"
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

// Outputs is the channel by which the output and debug operators of a query,
// whether in the query's flowgraph or in a subquery, send labeled vectors to
// the mux (see Mux).  A nil *Outputs discards the vectors sent to it.
type Outputs struct {
	ch chan muxresult
	mu sync.Mutex
	// labels holds the labels of the output operators.
	labels map[string]struct{}
}

func NewOutputs() *Outputs {
	return &Outputs{
		ch:     make(chan muxresult),
		labels: make(map[string]struct{}),
	}
}

func (o *Outputs) addLabel(label string) {
	if o == nil {
		return
	}
	// Output operators are created at runtime for the copies of a
	// correlated subquery (see Subquery), so this may race with others.
	o.mu.Lock()
	o.labels[label] = struct{}{}
	o.mu.Unlock()
}

func (o *Outputs) sortedLabels() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	var labels []string
	for label := range o.labels {
		labels = append(labels, label)
	}
	slices.Sort(labels)
	return labels
}

// send sends vec labeled label to the mux.
func (o *Outputs) send(ctx context.Context, label string, vec vector.Any) error {
	if o == nil {
		return nil
	}
	select {
	case o.ch <- muxresult{vec: vec, label: label}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type muxresult struct {
	vec   vector.Any
	label string
	err   error
	// pumped is true when a pump is done (with err if it failed).
	pumped bool
}

// Mux collects the outputs of a query.  Its output and debug operators send
// vectors on the query's Outputs, and the mux returns each vector labeled with
// the name of its output (see vector.Labeled).  The mux drives the leaves of
// the query's flowgraph, each of which ends in an output operator, with a pump
// for each leaf: a goroutine that pulls the leaf until EOS.  (An output
// operator in a subquery is driven by the subquery's evaluation.)
//
// Since the mux only reads from the Outputs channel and never pulls from the
// flowgraph, an operator's send always makes progress as long as the mux is
// pulled.
//
// When all of the pumps are done, no operator can send, so the mux returns an
// end of channel (a Labeled with a nil vector) for each output's label and
// then EOS, and it cancels the query.  There's no end of channel for debug.
type Mux struct {
	rctx    *runtime.Context
	once    sync.Once
	outputs *Outputs
	pumps   []vio.Puller
	running int
	ends    []string
}

// NewMux returns a Mux for outputs and the leaves of a query's flowgraph,
// pumps.
func NewMux(rctx *runtime.Context, outputs *Outputs, pumps []vio.Puller) *Mux {
	return &Mux{
		rctx:    rctx,
		outputs: outputs,
		pumps:   pumps,
		running: len(pumps),
	}
}

// Pull returns the next vector from any output.  The done argument is ignored
// since the mux doesn't pull from the flowgraph: to stop a query early, cancel
// its context (as exec.Query.Pull does).
func (m *Mux) Pull(bool) (vector.Any, error) {
	m.once.Do(func() {
		for _, p := range m.pumps {
			go m.pump(m.rctx.Context, NewCatcher(p))
		}
	})
	for m.running != 0 {
		select {
		case res := <-m.outputs.ch:
			if res.err != nil {
				m.rctx.Cancel()
				return nil, res.err
			}
			if !res.pumped {
				return &vector.Labeled{Any: res.vec, Label: res.label}, nil
			}
			m.running--
			if m.running == 0 {
				m.ends = m.outputs.sortedLabels()
			}
		case <-m.rctx.Context.Done():
			return nil, m.rctx.Context.Err()
		}
	}
	if len(m.ends) != 0 {
		label := m.ends[0]
		m.ends = m.ends[1:]
		return &vector.Labeled{Label: label}, nil
	}
	m.rctx.Cancel()
	return nil, nil
}

// pump pulls p until EOS or an error and then tells the mux that it's done.
// Since an output operator's send is synchronous, all of p's vectors have
// been received by the mux when p returns EOS.
func (m *Mux) pump(ctx context.Context, p vio.Puller) {
	var err error
	for {
		var vec vector.Any
		vec, err = p.Pull(false)
		if vec == nil || err != nil {
			break
		}
	}
	select {
	case m.outputs.ch <- muxresult{err: err, pumped: true}:
	case <-ctx.Done():
	}
}
