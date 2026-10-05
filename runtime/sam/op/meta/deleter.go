package meta

import (
	"errors"
	"sync"

	"github.com/segmentio/ksuid"
	"github.com/superdb/super"
	"github.com/superdb/super/db"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Deleter struct {
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
	deletes     *sync.Map
}

func NewDeleter(rctx *runtime.Context, parent vio.Puller, pool *db.Pool, pushdown sbuf.Pushdown, pruner expr.Evaluator, progress *vio.Progress, deletes *sync.Map) *Deleter {
	return &Deleter{
		parent:      parent,
		pushdown:    pushdown,
		pruner:      pruner,
		rctx:        rctx,
		pool:        pool,
		progress:    progress,
		unmarshaler: super.NewUnmarshaler(),
		deletes:     deletes,
	}
}

func (d *Deleter) Pull(done bool) (vector.Any, error) {
	if d.done {
		return nil, d.err
	}
	if done {
		if d.scanner != nil {
			_, err := d.scanner.Pull(true)
			d.close(err)
			d.scanner = nil
		}
		return nil, d.err
	}
	for {
		if d.scanner == nil {
			scanner, err := d.nextDeletion()
			if scanner == nil || err != nil {
				d.close(err)
				return nil, err
			}
			d.scanner = scanner
		}
		if vec, err := d.scanner.Pull(false); err != nil {
			d.close(err)
			return nil, err
		} else if vec != nil {
			return vec, nil
		}
		d.scanner = nil
	}
}

func (d *Deleter) nextDeletion() (vio.Puller, error) {
	for {
		if d.parent == nil { //XXX
			return nil, nil
		}
		// Pull the next object to be scanned.  It must be an object
		// not a partition.
		vec, err := d.parent.Pull(false)
		if vec == nil || err != nil {
			return nil, err
		}
		if vec.Len() != 1 {
			// We currently support only one partition per batch.
			return nil, errors.New("internal error: meta.Deleter encountered multi-valued batch")
		}
		val := vector.ValueAt(nil, vec, 0)
		if hasDeletes, err := d.hasDeletes(val); err != nil {
			return nil, err
		} else if !hasDeletes {
			continue
		}
		// Use a no-op progress so stats are not inflated.
		var progress vio.Progress
		scanner, object, err := newScanner(d.rctx.Context, d.rctx.Sctx, d.pool, d.unmarshaler, d.pruner, d.pushdown, &progress, val)
		if err != nil {
			return nil, err
		}
		d.deleteObject(object.ID)
		return scanner, nil
	}
}

func (d *Deleter) hasDeletes(val super.Value) (bool, error) {
	scanner, object, err := newScanner(d.rctx.Context, d.rctx.Sctx, d.pool, d.unmarshaler, d.pruner, d.pushdown, d.progress, val)
	if err != nil {
		return false, err
	}
	var count uint64
	for {
		vec, err := scanner.Pull(false)
		if err != nil {
			return false, err
		}
		if vec == nil {
			return count != object.Count, nil
		}
		count += uint64(vec.Len())
	}
}

func (d *Deleter) close(err error) {
	d.err = err
	d.done = true
}

func (d *Deleter) deleteObject(id ksuid.KSUID) {
	d.deletes.Store(id, nil)
}
