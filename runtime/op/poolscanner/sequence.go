package poolscanner

import (
	"context"
	"errors"

	"github.com/superdb/super"
	"github.com/superdb/super/db"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/op/merge"
	samexpr "github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/runtime/sam/op/meta"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// PoolScanner implements an op that pulls metadata partitions to scan
// from its parent and for each partition, scans the object.
type PoolScanner struct {
	parent      sbuf.Puller
	scanner     vio.Puller
	where       expr.Evaluator
	pruner      samexpr.Evaluator
	rctx        *runtime.Context
	pool        *db.Pool
	progress    *vio.Progress
	unmarshaler *super.Unmarshaler
	done        bool
	err         error
}

func NewPoolScanner(rctx *runtime.Context, parent sbuf.Puller, pool *db.Pool, where expr.Evaluator, pruner samexpr.Evaluator, progress *vio.Progress) *PoolScanner {
	return &PoolScanner{
		rctx:        rctx,
		parent:      parent,
		where:       where,
		pruner:      pruner,
		pool:        pool,
		progress:    progress,
		unmarshaler: super.NewUnmarshaler(),
	}
}

func (p *PoolScanner) Pull(done bool) (vector.Any, error) {
	if p.done {
		return nil, p.err
	}
	if done {
		if p.scanner != nil {
			_, err := p.scanner.Pull(true)
			p.close(err)
			p.scanner = nil
		}
		return nil, p.err
	}
	for {
		if p.scanner == nil {
			batch, err := p.parent.Pull(false)
			if batch == nil || err != nil {
				p.close(err)
				return nil, err
			}
			vals := batch.Values()
			if len(vals) != 1 {
				// We currently support only one partition per batch.
				err := errors.New("system error: SequenceScanner encountered multi-valued batch")
				p.close(err)
				return nil, err
			}
			p.scanner, _, err = newScanner(p.rctx.Context, p.rctx.Sctx, p.pool, p.unmarshaler, p.pruner, p.where, p.progress, vals[0])
			if err != nil {
				p.close(err)
				return nil, err
			}
		}
		vec, err := p.scanner.Pull(false)
		if err != nil {
			p.close(err)
			return nil, err
		}
		if vec != nil {
			return vec, nil
		}
		p.scanner = nil
	}
}

func (p *PoolScanner) close(err error) {
	p.err = err
	p.done = true
}

func newScanner(ctx context.Context, sctx *super.Context, pool *db.Pool, u *super.Unmarshaler, pruner samexpr.Evaluator, skip expr.Evaluator, progress *vio.Progress, val super.Value) (vio.Puller, *data.Object, error) {
	named, ok := val.Type().(*super.TypeNamed)
	if !ok {
		return nil, nil, errors.New("system error: SequenceScanner encountered unnamed object")
	}
	var objects []*data.Object
	if named.Name == "data.Object" {
		var object data.Object
		if err := u.Unmarshal(val, &object); err != nil {
			return nil, nil, err
		}
		objects = []*data.Object{&object}
	} else {
		var part meta.Partition
		if err := u.Unmarshal(val, &part); err != nil {
			return nil, nil, err
		}
		objects = part.Objects
	}
	scanner, err := newObjectsScanner(ctx, sctx, pool, objects, pruner, skip, progress)
	return scanner, objects[0], err
}

func newObjectsScanner(ctx context.Context, sctx *super.Context, pool *db.Pool, objects []*data.Object, pruner samexpr.Evaluator, skip expr.Evaluator, progress *vio.Progress) (vio.Puller, error) {
	pullers := make([]vio.Puller, 0, len(objects))
	pullersDone := func() {
		for _, puller := range pullers {
			puller.Pull(true)
		}
	}
	for _, object := range objects {
		s, err := newObjectScanner(ctx, sctx, pool, object, skip, progress)
		if err != nil {
			pullersDone()
			return nil, err
		}
		pullers = append(pullers, s)
	}
	if len(pullers) == 1 {
		return pullers[0], nil
	}
	return merge.NewMerge(ctx, pullers, db.ImportComparator(sctx, pool).Compare), nil
}

func newObjectScanner(ctx context.Context, sctx *super.Context, pool *db.Pool, object *data.Object, skip expr.Evaluator, progress *vio.Progress) (vio.Puller, error) {
	scanner, err := pool.NewReader(ctx, sctx, object, nil) //XXX pushdown API needs updating
	if err != nil {
		return nil, err
	}
	return &statScanner{
		scanner:  scanner,
		progress: progress,
		skip:     skip,
	}, nil
}

type statScanner struct {
	scanner  vio.ScanCloser
	err      error
	progress *vio.Progress
	skip     expr.Evaluator
}

func (s *statScanner) Pull(done bool) (vector.Any, error) {
	if s.scanner == nil {
		return nil, s.err
	}
	vec, err := s.scanner.Pull(done)
	if vec == nil || err != nil {
		s.progress.Add(s.scanner.Progress())
		if err2 := s.scanner.Close(); err == nil {
			err = err2
		}
		s.err = err
		s.scanner = nil
		return vec, err
	}
	if s.skip != nil {
		if keep, ok := applySkip(vec, s.skip.Eval(vec)); ok {
			return keep, nil
		}
	}
	return vec, err
}

func (s *statScanner) Close() error {
	return nil
}

func applySkip(vec, skip vector.Any) (vector.Any, bool) {
	// errors are ignored for filters
	keep, _ := expr.BoolMask(skip)
	keep.Flip(0, uint64(vec.Len()))
	if keep.IsEmpty() {
		return nil, false
	}
	if keep.GetCardinality() == uint64(vec.Len()) {
		return vec, true
	}
	return vector.Pick(vec, keep.ToArray()), true
}
