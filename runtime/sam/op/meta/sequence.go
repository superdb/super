package meta

import (
	"context"
	"errors"
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup/oldbsup"
	"github.com/superdb/super/db"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/runtime/sam/op/merge"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// SequenceScanner implements an op that pulls metadata partitions to scan
// from its parent and for each partition, scans the object.
type SequenceScanner struct {
	parent      vio.Puller
	scanner     vio.Puller
	pushdown    sbuf.Pushdown
	pruner      expr.Evaluator
	rctx        *runtime.Context
	pool        *db.Pool
	progress    *vio.Progress
	unmarshaler *super.Unmarshaler
	done        bool
	err         error
}

func NewSequenceScanner(rctx *runtime.Context, parent vio.Puller, pool *db.Pool, pushdown sbuf.Pushdown, pruner expr.Evaluator, progress *vio.Progress) *SequenceScanner {
	return &SequenceScanner{
		rctx:        rctx,
		parent:      parent,
		pushdown:    pushdown,
		pruner:      pruner,
		pool:        pool,
		progress:    progress,
		unmarshaler: super.NewUnmarshaler(),
	}
}

func (s *SequenceScanner) Pull(done bool) (vector.Any, error) {
	if s.done {
		return nil, s.err
	}
	if done {
		if s.scanner != nil {
			_, err := s.scanner.Pull(true)
			s.close(err)
			s.scanner = nil
		}
		return nil, s.err
	}
	for {
		if s.scanner == nil {
			vec, err := s.parent.Pull(false)
			if vec == nil || err != nil {
				s.close(err)
				return nil, err
			}
			if vec.Len() != 1 {
				// We currently support only one partition per batch.
				err := errors.New("system error: SequenceScanner encountered multi-valued batch")
				s.close(err)
				return nil, err
			}
			val := vector.ValueAt(nil, vec, 0)
			s.scanner, _, err = newScanner(s.rctx.Context, s.rctx.Sctx, s.pool, s.unmarshaler, s.pruner, s.pushdown, s.progress, val)
			if err != nil {
				s.close(err)
				return nil, err
			}
		}
		vec, err := s.scanner.Pull(false)
		if err != nil {
			s.close(err)
			return nil, err
		}
		if vec != nil {
			return vec, nil
		}
		s.scanner = nil
	}
}

func (s *SequenceScanner) close(err error) {
	s.err = err
	s.done = true
}

type SearchScanner struct {
	pushdown sbuf.Pushdown
	parent   Searcher
	pool     *db.Pool
	progress *vio.Progress
	rctx     *runtime.Context
	scanner  sbuf.Puller
}

type Searcher interface {
	Pull(bool) (*data.Object, *vector.Bool, error)
}

func NewSearchScanner(rctx *runtime.Context, parent Searcher, pool *db.Pool, pushdown sbuf.Pushdown, progress *vio.Progress) *SearchScanner {
	return &SearchScanner{
		pushdown: pushdown,
		parent:   parent,
		pool:     pool,
		progress: progress,
		rctx:     rctx,
	}
}

func (s *SearchScanner) Pull(done bool) (sbuf.Batch, error) {
	if done {
		var err error
		if s.scanner != nil {
			_, err = s.scanner.Pull(true)
			s.scanner = nil
		}
		return nil, err
	}
	for {
		if s.scanner == nil {
			o, b, err := s.parent.Pull(done)
			if b == nil || err != nil {
				return nil, err
			}
			s.scanner, err = newObjectScanner(s.rctx.Context, s.rctx.Sctx, s.pool, o, s.pushdown, s.progress)
			if err != nil {
				return nil, err
			}
		}
		batch, err := s.scanner.Pull(false)
		if err != nil {
			return nil, err
		}
		if batch != nil {
			return batch, nil
		}
		s.scanner = nil
	}
}

func newScanner(ctx context.Context, sctx *super.Context, pool *db.Pool, u *super.Unmarshaler, pruner expr.Evaluator, pushdown sbuf.Pushdown, progress *vio.Progress, val super.Value) (vio.Puller, *data.Object, error) {
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
		var part Partition
		if err := u.Unmarshal(val, &part); err != nil {
			return nil, nil, err
		}
		objects = part.Objects
	}
	scanner, err := newObjectsScanner(ctx, sctx, pool, objects, pruner, pushdown, progress)
	return scanner, objects[0], err
}

func newObjectsScanner(ctx context.Context, sctx *super.Context, pool *db.Pool, objects []*data.Object, pruner expr.Evaluator, pushdown sbuf.Pushdown, progress *vio.Progress) (vio.Puller, error) {
	pullers := make([]vio.Puller, 0, len(objects))
	pullersDone := func() {
		for _, puller := range pullers {
			puller.Pull(true)
		}
	}
	for _, object := range objects {
		s, err := newObjectScanner(ctx, sctx, pool, object, pushdown, progress)
		if err != nil {
			pullersDone()
			return nil, err
		}
		pullers = append(pullers, s)
	}
	if len(pullers) == 1 {
		return pullers[0], nil
	}
	return merge.New(ctx, pullers, db.ImportComparator(sctx, pool).Compare), nil
}

func newObjectScanner(ctx context.Context, sctx *super.Context, pool *db.Pool, object *data.Object, pushdown sbuf.Pushdown, progress *vio.Progress) (vio.Puller, error) {
	rc, err := object.NewReader(ctx, pool.Storage(), pool.DataPath)
	if err != nil {
		return nil, err
	}
	scanner, err := oldbsup.NewReader(sctx, rc).NewScanner(ctx, pushdown)
	if err != nil {
		rc.Close()
		return nil, err
	}
	return &statScanner{
		scanner:  scanner,
		closer:   rc,
		progress: progress,
	}, nil
}

type statScanner struct {
	scanner  sbuf.Scanner
	closer   io.Closer
	err      error
	progress *vio.Progress
}

func (s *statScanner) Pull(done bool) (sbuf.Batch, error) {
	if s.scanner == nil {
		return nil, s.err
	}
	batch, err := s.scanner.Pull(done)
	if batch == nil || err != nil {
		s.progress.Add(s.scanner.Progress())
		if err2 := s.closer.Close(); err == nil {
			err = err2
		}
		s.err = err
		s.scanner = nil
	}
	return batch, err
}
