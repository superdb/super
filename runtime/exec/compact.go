package exec

import (
	"context"
	"errors"

	"github.com/segmentio/ksuid"
	"github.com/superdb/super"
	"github.com/superdb/super/db"
	"github.com/superdb/super/db/commits"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/sam/op/meta"
	"github.com/superdb/super/sbuf"
)

func Compact(ctx context.Context, _ *db.Root, pool *db.Pool, branchName string, objectIDs []ksuid.KSUID, author, message, info string) (ksuid.KSUID, error) {
	if len(objectIDs) < 2 {
		return ksuid.Nil, errors.New("compact: two or more source objects required")
	}
	branch, err := pool.OpenBranchByName(ctx, branchName)
	if err != nil {
		return ksuid.Nil, err
	}
	base, err := pool.Snapshot(ctx, branch.Commit)
	if err != nil {
		return ksuid.Nil, err
	}
	compact := commits.NewSnapshot()
	for _, oid := range objectIDs {
		o, err := base.Lookup(oid)
		if err != nil {
			return ksuid.Nil, err
		}
		compact.AddDataObject(o)
	}
	sctx := super.NewContext()
	lister := meta.NewSortedListerFromSnap(ctx, super.NewContext(), pool, compact, nil)
	rctx := runtime.NewContext(ctx, sctx)
	slicer := meta.NewSlicer(lister, sctx)
	puller := meta.NewSequenceScanner(rctx, slicer, pool, nil, nil, nil)
	w := db.NewSortedWriter(ctx, sctx, pool)
	if err := sbuf.CopyPuller(w, puller); err != nil {
		puller.Pull(true)
		w.Abort()
		return ksuid.Nil, err
	}
	if err := w.Close(); err != nil {
		w.Abort()
		return ksuid.Nil, err
	}
	commit, err := branch.CommitCompact(ctx, compact.SelectAll(), w.Objects(), author, message, info)
	if err != nil {
		w.Abort()
		return ksuid.Nil, err
	}
	return commit, nil
}
