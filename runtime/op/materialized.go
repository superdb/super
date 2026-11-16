package op

import (
	"sync"

	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Materialized struct {
	mu     sync.Mutex
	parent vio.Puller
	eos    bool
	err    error
	vecs   []vector.Any
}

func NewMaterialized(parent vio.Puller) *Materialized {
	return &Materialized{parent: parent}
}

func (m *Materialized) NewPuller() vio.Puller {
	return &materializedPuller{m: m}
}

func (m *Materialized) pull(i int) (vector.Any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i >= len(m.vecs) && !m.eos && m.err == nil {
		if vec, err := m.parent.Pull(false); err != nil {
			m.err = err
		} else if vec == nil {
			m.eos = true
		} else {
			m.vecs = append(m.vecs, vec)
		}
	}
	if i < len(m.vecs) {
		return m.vecs[i], nil
	}
	return nil, m.err
}

type materializedPuller struct {
	m    *Materialized
	next int
}

func (r *materializedPuller) Pull(done bool) (vector.Any, error) {
	if done {
		r.next = 0
		return nil, nil
	}
	vec, err := r.m.pull(r.next)
	if vec == nil || err != nil {
		r.next = 0
		return nil, err
	}
	r.next++
	return vec, nil
}
