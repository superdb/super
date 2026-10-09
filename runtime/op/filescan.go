package op

import (
	"fmt"
	"os"
	"sync"

	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type FileScan struct {
	rctx     *runtime.Context
	env      *exec.Environment
	parent   vio.Puller
	paths    []string
	format   string
	pushdown vio.Pushdown
	progress *vio.Progress

	mu                   sync.Mutex
	current              exec.ConcurrentPuller
	eos                  bool
	inputValuesRemaining int
	nextPath             int
	numDone              int
	puller               vio.Puller
	pullers              []*concurrentPuller
}

func NewFileScan(rctx *runtime.Context, env *exec.Environment, parent vio.Puller, paths []string, format string, p vio.Pushdown, progress *vio.Progress) *FileScan {
	return &FileScan{
		rctx:     rctx,
		env:      env,
		parent:   parent,
		paths:    paths,
		format:   format,
		pushdown: p,
		progress: progress,
	}
}

func (f *FileScan) Pull(done bool) (vector.Any, error) {
	if f.puller == nil {
		if len(f.pullers) > 0 {
			panic("Pull called after ConcurrentPullers")
		}
		f.puller = f.NewConcurrentPullers(1)[0]
	}
	return f.puller.Pull(done)
}

func (f *FileScan) NewConcurrentPullers(n int) []vio.Puller {
	if n < 1 {
		panic(n)
	}
	if len(f.pullers) > 0 {
		panic("ConcurrentPullers called after Pull or called twice")
	}
	var out []vio.Puller
	for i := range n {
		p := newPuller(f, i)
		f.pullers = append(f.pullers, p)
		out = append(out, p)
	}
	return out
}

func (f *FileScan) done() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.numDone++
	if f.numDone == len(f.pullers) {
		f.current = nil
		if !f.eos {
			_, err := f.parent.Pull(true)
			return err
		}
		f.eos = false
		f.inputValuesRemaining = 0
		f.numDone = 0
		for _, p := range f.pullers {
			p.waitCh <- struct{}{}
		}
	}
	return nil
}

func (f *FileScan) nextFile(current exec.ConcurrentPuller) (exec.ConcurrentPuller, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.eos {
		return nil, nil
	}
	if current != f.current {
		return f.current, nil
	}
	for {
		for f.inputValuesRemaining == 0 {
			vec, err := f.parent.Pull(false)
			if vec == nil || err != nil {
				f.eos = true
				return nil, err
			}
			f.inputValuesRemaining = int(vec.Len())
		}
		puller, err := f.openNextPath()
		if puller != nil || err != nil {
			return puller, err
		}
		f.inputValuesRemaining--
		f.nextPath = 0
	}
}

func (f *FileScan) openNextPath() (exec.ConcurrentPuller, error) {
	for f.nextPath < len(f.paths) {
		path := f.paths[f.nextPath]
		f.nextPath++
		puller, err := f.env.Open(f.rctx.Context, f.rctx.Sctx, path, f.format, f.pushdown, f.progress, len(f.pullers))
		if err != nil {
			if f.env.IgnoreOpenErrors {
				fmt.Fprintln(os.Stderr, err)
				continue
			}
			return nil, err
		}
		f.current = puller
		return puller, nil
	}
	return nil, nil
}

type concurrentPuller struct {
	f  *FileScan
	id int

	eos     bool
	current exec.ConcurrentPuller
	waitCh  chan struct{}
}

func newPuller(f *FileScan, id int) *concurrentPuller {
	return &concurrentPuller{f: f, id: id, waitCh: make(chan struct{}, 1)}
}

func (p *concurrentPuller) Pull(done bool) (vector.Any, error) {
	if done {
		p.f.done()
		p.eos = true
		p.current = nil
		return nil, nil
	}
	if p.eos {
		p.eos = false
		select {
		case <-p.waitCh:
		case <-p.f.rctx.Context.Done():
			return nil, p.f.rctx.Context.Err()
		}
	}
	for {
		if err := p.f.rctx.Context.Err(); err != nil {
			return nil, err
		}
		if p.current != nil {
			vec, err := p.current.ConcurrentPull(false, p.id)
			if vec != nil || err != nil {
				return vec, err
			}
		}
		puller, err := p.f.nextFile(p.current)
		if err != nil {
			return nil, err
		}
		if puller == nil {
			return p.Pull(true)
		}
		p.current = puller
	}
}
