package op

import (
	"context"
	"errors"

	"github.com/superdb/super"
	"github.com/superdb/super/db"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/expr"
	samexpr "github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

// FilterFactory returns a new filter evaluator.  The objects of a partition are
// scanned concurrently and evaluators may cache state, so each object scanner
// gets its own evaluator.
type FilterFactory func() (expr.Evaluator, error)

// TableScanner implements an op that pulls metadata partitions to scan
// from its parent and for each partition, scans the object.
type TableScanner struct {
	parent      vio.Puller
	scanner     vio.Puller
	where       FilterFactory
	pruner      samexpr.Evaluator
	rctx        *runtime.Context
	table       *db.Table
	progress    *vio.Progress
	unmarshaler *super.Unmarshaler
	done        bool
	err         error
}

func NewTableScanner(rctx *runtime.Context, parent vio.Puller, table *db.Table, where FilterFactory, pruner samexpr.Evaluator, progress *vio.Progress) *TableScanner {
	u := super.NewUnmarshaler()
	u.SetContext(rctx.Sctx)
	return &TableScanner{
		rctx:        rctx,
		parent:      parent,
		where:       where,
		pruner:      pruner,
		table:       table,
		progress:    progress,
		unmarshaler: u,
	}
}

func (t *TableScanner) Pull(done bool) (vector.Any, error) {
	if t.done {
		return nil, t.err
	}
	if done {
		if t.scanner != nil {
			_, err := t.scanner.Pull(true)
			t.close(err)
			t.scanner = nil
		}
		return nil, t.err
	}
	for {
		if t.scanner == nil {
			vec, err := t.parent.Pull(false)
			if vec == nil || err != nil {
				t.close(err)
				return nil, err
			}
			if vec.Len() != 1 {
				// We currently support only one partition per batch.
				err := errors.New("system error: SequenceScanner encountered multi-valued batch")
				t.close(err)
				return nil, err
			}
			val := vector.ValueAt(nil, vec, 0)
			t.scanner, _, err = newScanner(t.rctx.Context, t.rctx.Sctx, t.table, t.unmarshaler, t.pruner, t.where, t.progress, val)
			if err != nil {
				t.close(err)
				return nil, err
			}
		}
		vec, err := t.scanner.Pull(false)
		if err != nil {
			t.close(err)
			return nil, err
		}
		if vec != nil {
			return vec, nil
		}
		t.scanner = nil
	}
}

func (t *TableScanner) close(err error) {
	t.err = err
	t.done = true
}

func newScanner(ctx context.Context, sctx *super.Context, table *db.Table, u *super.Unmarshaler, pruner samexpr.Evaluator, where FilterFactory, progress *vio.Progress, val super.Value) (vio.Puller, *data.Object, error) {
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
		var part data.Partition
		if err := u.Unmarshal(val, &part); err != nil {
			return nil, nil, err
		}
		objects = part.Objects
	}
	scanner, err := newObjectsScanner(ctx, sctx, table, objects, pruner, where, progress)
	return scanner, objects[0], err
}

func newObjectsScanner(ctx context.Context, sctx *super.Context, table *db.Table, objects []*data.Object, pruner samexpr.Evaluator, newFilter FilterFactory, progress *vio.Progress) (vio.Puller, error) {
	pullers := make([]vio.Puller, 0, len(objects))
	pullersDone := func() {
		for _, puller := range pullers {
			puller.Pull(true)
		}
	}
	for _, object := range objects {
		var filter expr.Evaluator
		if newFilter != nil {
			var err error
			if filter, err = newFilter(); err != nil {
				pullersDone()
				return nil, err
			}
		}
		s, err := newObjectScanner(ctx, sctx, table, object, filter, progress)
		if err != nil {
			pullersDone()
			return nil, err
		}
		pullers = append(pullers, s)
	}
	if len(pullers) == 1 {
		return pullers[0], nil
	}
	return NewMerge(ctx, pullers, db.ImportComparator(sctx, table).Compare), nil
}

func newObjectScanner(ctx context.Context, sctx *super.Context, table *db.Table, object *data.Object, filter expr.Evaluator, progress *vio.Progress) (vio.Puller, error) {
	reader, err := table.NewReader(ctx, sctx, object, nil, progress, 1)
	if err != nil {
		return nil, err
	}
	return &filterScanner{
		reader: reader,
		filter: filter,
	}, nil
}

type filterScanner struct {
	reader vio.PullCloser
	err    error
	filter expr.Evaluator
}

func (f *filterScanner) Pull(done bool) (vector.Any, error) {
	for {
		if f.reader == nil {
			return nil, f.err
		}
		vec, err := f.reader.Pull(done)
		if vec == nil || err != nil {
			if err2 := f.reader.Close(); err == nil {
				err = err2
			}
			f.err = err
			f.reader = nil
			return vec, err
		}
		if f.filter == nil {
			return vec, err
		}
		if masked, ok := applyMask(vec, f.filter.Eval(vec)); ok {
			return masked, nil
		}
	}
}

func (*filterScanner) Close() error {
	return nil
}
