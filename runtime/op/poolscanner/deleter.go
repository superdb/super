package poolscanner

import (
	"sync"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/db"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/expr"
	samexpr "github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Deleter struct {
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
	deletes     *sync.Map
}

func NewDeleter(rctx *runtime.Context, parent sbuf.Puller, pool *db.Pool, where expr.Evaluator, pruner samexpr.Evaluator, progress *vio.Progress, deletes *sync.Map) *Deleter {
	return &Deleter{
		parent:      parent,
		where:       where,
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
		if batch, err := d.scanner.Pull(false); err != nil {
			d.close(err)
			return nil, err
		} else if batch != nil {
			return batch, nil
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
		batch, err := d.parent.Pull(false)
		if batch == nil || err != nil {
			return nil, err
		}
		vals := batch.Values()
		if len(vals) != 1 {
			// We currently support only one partition per batch.
		}
		if hasDeletes, err := d.hasDeletes(vals[0]); err != nil {
			return nil, err
		} else if !hasDeletes {
			continue
		}
		// Use a no-op progress so stats are not inflated.
		var progress vio.Progress
		scanner, object, err := newScanner(d.rctx.Context, d.rctx.Sctx, d.pool, d.unmarshaler, d.pruner, d.where, &progress, vals[0])
		if err != nil {
			return nil, err
		}
		d.deleteObject(object.ID)
		return scanner, nil
	}
}

func (d *Deleter) hasDeletes(val super.Value) (bool, error) {
	scanner, object, err := newScanner(d.rctx.Context, d.rctx.Sctx, d.pool, d.unmarshaler, d.pruner, d.where, d.progress, val)
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

func (d *Deleter) deleteObject(id uuid.UUID) {
	d.deletes.Store(id, nil)
}
