package sbuf

import (
	"sync"

	"github.com/superdb/super"
	"github.com/superdb/super/scode"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Materializer struct {
	parent vio.Puller
}

var _ Puller = (*Materializer)(nil)

func NewMaterializer(p vio.Puller) Puller {
	return &Materializer{
		parent: p,
	}
}

func (m *Materializer) Pull(done bool) (Batch, error) {
	for {
		vec, err := m.parent.Pull(done)
		if _, ok := vec.(*vector.Control); ok {
			continue
		}
		vec, _ = vector.Unlabel(vec)
		if vec == nil || err != nil {
			return nil, err
		}
		return Materialize(vec), nil
	}
}

func Materialize(vec vector.Any) Batch {
	if vec == nil {
		return nil
	}
	// Labels are emitted by the query API and should be peeled off before
	// arriving here but some of our tests don't do so. XXX not true anymore?
	if labeled, ok := vec.(*vector.Labeled); ok {
		vec = labeled.Any
	}
	if vec == nil || vec.Len() == 0 {
		return nil
	}
	var sb scode.Builder
	vals := make([]super.Value, vec.Len())
	for i := range vec.Len() {
		vals[i] = vector.ValueAt(&sb, vec, i).Copy()
	}
	return NewArray(vals)
}

func Dematerialize(sctx *super.Context, vals ...super.Value) vector.Any {
	builder := vector.NewDynamicValueBuilder()
	for _, val := range vals {
		builder.Write(val)
	}
	return builder.Build(sctx)
}

func ValToPuller(sctx *super.Context, val super.Value) vio.Puller {
	builder := vector.NewDynamicValueBuilder()
	builder.Write(val)
	return &vecPuller{builder.Build(sctx)}
}

type vecPuller struct {
	vec vector.Any
}

func (v *vecPuller) Pull(bool) (vector.Any, error) {
	vec := v.vec
	v.vec = nil
	return vec, nil
}

type Dematerializer struct {
	sctx   *super.Context
	mu     sync.Mutex
	parent Puller
}

func NewDematerializer(sctx *super.Context, p Puller) *Dematerializer {
	return &Dematerializer{sctx: sctx, parent: p}
}

func (d *Dematerializer) Pull(done bool) (vector.Any, error) {
	return d.ConcurrentPull(done, 0)
}

func (d *Dematerializer) ConcurrentPull(done bool, _ int) (vector.Any, error) {
	batch, err := d.parentPull(done)
	if batch == nil || err != nil {
		return nil, err
	}
	defer batch.Unref()
	builder := vector.NewDynamicValueBuilder()
	for _, val := range batch.Values() {
		builder.Write(val)
	}
	return builder.Build(d.sctx), nil
}

func (d *Dematerializer) parentPull(done bool) (Batch, error) {
	d.mu.Lock()
	// Defer to ensure lock is released if d.parent.Pull panics.
	defer d.mu.Unlock()
	return d.parent.Pull(done)
}

func WriteVec(w sio.Writer, vec vector.Any) error {
	for _, val := range Materialize(vec).Values() {
		if err := w.Write(val); err != nil {
			return err
		}
	}
	return nil
}
