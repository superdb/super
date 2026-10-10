package op

import (
	"context"
	"fmt"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/db"
	"github.com/superdb/super/db/commits"
	"github.com/superdb/super/order"
	samexpr "github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/vector/vio"
)

func NewDBMetaScanner(ctx context.Context, sctx *super.Context, r *db.Root, meta string) (vio.Puller, error) {
	var vals []super.Value
	var err error
	switch meta {
	case "pools":
		vals, err = r.BatchifyPools(ctx, sctx, nil)
	case "branches":
		vals, err = r.BatchifyBranches(ctx, sctx, nil)
	default:
		return nil, fmt.Errorf("unknown database metadata type: %q", meta)
	}
	if err != nil {
		return nil, err
	}
	return vio.NewPuller(sbuf.Dematerialize(sctx, vals...)), nil
}

func NewPoolMetaScanner(ctx context.Context, sctx *super.Context, r *db.Root, poolID uuid.UUID, meta string) (vio.Puller, error) {
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
	return vio.NewPuller(sbuf.Dematerialize(sctx, vals...)), nil
}

func NewCommitMetaScanner(ctx context.Context, sctx *super.Context, r *db.Root, poolID, commit uuid.UUID, meta string, pruner samexpr.Evaluator) (vio.Puller, error) {
	p, err := r.OpenPool(ctx, poolID)
	if err != nil {
		return nil, err
	}
	switch meta {
	case "objects":
		lister, err := NewLister(ctx, sctx, p, commit, pruner)
		if err != nil {
			return nil, err
		}
		return lister, nil
	case "partitions":
		lister, err := NewLister(ctx, sctx, p, commit, pruner)
		if err != nil {
			return nil, err
		}
		slicer, err := NewSlicer(sctx, lister), nil
		if err != nil {
			return nil, err
		}
		return slicer, nil
	case "log":
		tips, err := p.BatchifyBranchTips(ctx, sctx, nil)
		if err != nil {
			return nil, err
		}
		tipsPuller := vio.NewPuller(sbuf.Dematerialize(sctx, tips...))
		log := p.OpenCommitLog(ctx, sctx, commit)
		logPuller := sbuf.NewDematerializer(sctx, sbuf.NewPuller(log))
		return vio.ConcatPuller(tipsPuller, logPuller), nil
	case "rawlog":
		reader, err := p.OpenCommitLogAsBSUP(ctx, sctx, commit)
		if err != nil {
			return nil, err
		}
		return sbuf.NewDematerializer(sctx, sbuf.NewPuller(reader)), nil
	default:
		return nil, fmt.Errorf("unknown commit metadata type: %q", meta)
	}
}

func objectReader(sctx *super.Context, snap commits.View, order order.Which) (sio.Reader, error) {
	objects := snap.Select(nil, order)
	m := super.NewMarshaler(sctx)
	m.Decorate(super.StylePackage)
	return readerFunc(func() (*super.Value, error) {
		if len(objects) == 0 {
			return nil, nil
		}
		val, err := m.Marshal(objects[0])
		objects = objects[1:]
		return &val, err
	}), nil
}

type readerFunc func() (*super.Value, error)

func (r readerFunc) Read() (*super.Value, error) { return r() }
