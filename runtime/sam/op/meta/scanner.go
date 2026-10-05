package meta

import (
	"context"
	"fmt"

	"github.com/segmentio/ksuid"
	"github.com/superdb/super"
	"github.com/superdb/super/db"
	"github.com/superdb/super/db/commits"
	"github.com/superdb/super/order"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

func NewDBMetaScanner(ctx context.Context, sctx *super.Context, r *db.Root, meta string) (vio.Scanner, error) {
	var vals []super.Value
	var err error
	switch meta {
	case "pools":
		vals, err = r.MarshalPools(ctx, sctx, nil)
	case "branches":
		vals, err = r.MarshalBranches(ctx, sctx, nil)
	default:
		return nil, fmt.Errorf("unknown database metadata type: %q", meta)
	}
	if err != nil {
		return nil, err
	}
	return &progress{
		Puller: vio.NewPuller(sbuf.Dematerialize(sctx, vals...)),
	}, nil
}

type progress struct {
	vio.Puller
	progress vio.Progress
}

func (p *progress) Pull(done bool) (vector.Any, error) {
	vec, err := p.Puller.Pull(done)
	if vec == nil || err != nil {
		return vec, err
	}
	p.progress.RecordsRead += int64(vec.Len())
	return vec, nil
}

func (p *progress) Progress() vio.Progress {
	return p.progress
}

func NewPoolMetaScanner(ctx context.Context, sctx *super.Context, r *db.Root, poolID ksuid.KSUID, meta string) (sbuf.Scanner, error) {
	p, err := r.OpenPool(ctx, poolID)
	if err != nil {
		return nil, err
	}
	var vals []super.Value
	switch meta {
	case "branches":
		m := super.NewMarshaler(sctx)
		m.Decorate(super.StylePackage)
		vals, err = p.BatchifyBranches(ctx, sctx, nil, m, nil)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown pool metadata type: %q", meta)
	}
	return sbuf.NewScanner(ctx, sbuf.NewArray(vals), nil)
}

func NewCommitMetaScanner(ctx context.Context, sctx *super.Context, r *db.Root, poolID, commit ksuid.KSUID, meta string, pruner expr.Evaluator) (vio.Puller, error) {
	p, err := r.OpenPool(ctx, poolID)
	if err != nil {
		return nil, err
	}
	switch meta {
	case "objects":
		return NewSortedLister(ctx, sctx, p, commit, pruner)
	case "partitions":
		lister, err := NewSortedLister(ctx, sctx, p, commit, pruner)
		if err != nil {
			return nil, err
		}
		return NewSlicer(lister, sctx), nil
	case "log":
		tips, err := p.BatchifyBranchTips(ctx, sctx, nil)
		if err != nil {
			return nil, err
		}
		tipsScanner := &progress{Puller: vio.NewPuller(sbuf.Dematerialize(sctx, tips...))}
		log := p.OpenCommitLog(ctx, sctx, commit)
		logScanner := &progress{Puller: sbuf.NewDematerializer(sctx, sbuf.NewPuller(log))}
		return vio.MultiScanner(tipsScanner, logScanner), nil
	case "rawlog":
		reader, err := p.OpenCommitLogAsBSUP(ctx, sctx, commit)
		if err != nil {
			return nil, err
		}
		return &progress{Puller: sbuf.NewDematerializer(sctx, sbuf.NewPuller(reader))}, nil
	case "vectors":
		snap, err := p.Snapshot(ctx, commit)
		if err != nil {
			return nil, err
		}
		vectors := commits.Vectors(snap)
		reader, err := objectReader(sctx, vectors, p.SortKeys.Primary().Order)
		if err != nil {
			return nil, err
		}
		return &progress{Puller: reader}, nil
	default:
		return nil, fmt.Errorf("unknown commit metadata type: %q", meta)
	}
}

func objectReader(sctx *super.Context, snap commits.View, order order.Which) (vio.Puller, error) {
	objects := snap.Select(nil, order)
	m := super.NewMarshaler(sctx)
	m.Decorate(super.StylePackage)
	return readerFunc(func() (vector.Any, error) {
		if len(objects) == 0 {
			return nil, nil
		}
		val, err := m.Marshal(objects[0])
		objects = objects[1:]
		return sbuf.Dematerialize(sctx, val), err
	}), nil
}

type readerFunc func() (vector.Any, error)

func (r readerFunc) Pull(bool) (vector.Any, error) { return r() }
