package op

import (
	"context"
	"sync"

	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type forwarder interface {
	forward(vector.Any) bool
}

type router struct {
	ctx       context.Context
	forwarder forwarder
	parent    vio.Puller

	routes   []*route
	nblocked int
	once     sync.Once
}

func newRouter(ctx context.Context, f forwarder, parent vio.Puller) *router {
	return &router{ctx: ctx, forwarder: f, parent: parent}
}

func (r *router) addRoute() *route {
	route := &route{r, make(chan result), make(chan struct{}), false}
	r.routes = append(r.routes, route)
	return route
}

func (r *router) run() {
	for {
		if r.nblocked == len(r.routes) {
			// Send done upstream.
			if _, err := r.parent.Pull(true); err != nil {
				for _, route := range r.routes {
					select {
					case route.resultCh <- result{nil, err}:
					case <-r.ctx.Done():
					}
				}
				return
			}
			r.unblockBranches()
		}
		vec, err := r.parent.Pull(false)
		if vec != nil && err == nil {
			if !r.forwarder.forward(vec) {
				return
			}
			continue
		}
		for _, route := range r.routes {
			if !route.send(vec, err) {
				return
			}
		}
		if vec == nil && err == nil {
			// EOS unblocks all branches.
			r.unblockBranches()
		}
	}
}

func (r *router) unblockBranches() {
	for _, route := range r.routes {
		route.blocked = false
	}
	r.nblocked = 0
}

type route struct {
	router   *router
	resultCh chan result
	doneCh   chan struct{}
	blocked  bool
}

type result struct {
	vector vector.Any
	err    error
}

func (r *route) Pull(done bool) (vector.Any, error) {
	r.router.once.Do(func() { go r.router.run() })
	if done {
		select {
		case r.doneCh <- struct{}{}:
			return nil, nil
		case <-r.router.ctx.Done():
			return nil, r.router.ctx.Err()
		}
	}
	select {
	case r := <-r.resultCh:
		return r.vector, r.err
	case <-r.router.ctx.Done():
		return nil, r.router.ctx.Err()
	}
}

func (r *route) send(vec vector.Any, err error) bool {
	if r.blocked {
		return true
	}
	select {
	case r.resultCh <- result{vec, err}:
	case <-r.doneCh:
		r.blocked = true
		r.router.nblocked++
	case <-r.router.ctx.Done():
		return false
	}
	return true
}
