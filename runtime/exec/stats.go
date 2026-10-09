package exec

import (
	"context"

	"github.com/superdb/super"
	"github.com/superdb/super/db"
	"github.com/superdb/super/db/commits"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/runtime/sam/expr/extent"
)

// XXX for backward compat keep this for now, and return branchstats for pool/main
type PoolStats struct {
	Size int64 `super:"size"`
	// XXX (nibs) - This shouldn't be a span because keys don't have to be time.
	Span *nano.Span `super:"span"`
}

func GetPoolStats(ctx context.Context, p *db.Pool, snap commits.View) (info PoolStats, err error) {
	// XXX this doesn't scale... it should be stored in the snapshot and is
	// not easy to compute in the face of deletes...
	var poolSpan *extent.Generic
	sortKey, ok := p.SortKeys.Primary()
	if !ok {
		for _, object := range snap.Select(nil, order.Asc) {
			info.Size += object.Size
		}
	} else {
		for _, object := range snap.Select(nil, sortKey.Order) {
			info.Size += object.Size
			if poolSpan == nil {
				poolSpan = extent.NewGenericFromOrder(object.Min, object.Max, order.Asc)
			} else {
				poolSpan.Extend(object.Min)
				poolSpan.Extend(object.Max)
			}
		}
	}
	//XXX need to change API to take return key range
	if poolSpan != nil {
		min := poolSpan.First()
		if min.Type() == super.TypeTime {
			firstTs := super.DecodeTime(min.Bytes())
			lastTs := super.DecodeTime(poolSpan.Last().Bytes())
			if lastTs < firstTs {
				firstTs, lastTs = lastTs, firstTs
			}
			span := nano.NewSpanTs(firstTs, lastTs+1)
			info.Span = &span
		}
	}
	return info, err
}

type BranchStats struct {
	Size int64 `super:"size"`
	// XXX (nibs) - This shouldn't be a span because keys don't have to be time.
	Span *nano.Span `super:"span"`
}

func GetBranchStats(ctx context.Context, b *db.Branch, snap commits.View) (info BranchStats, err error) {
	// XXX this doesn't scale... it should be stored in the snapshot and is
	// not easy to compute in the face of deletes...
	var poolSpan *extent.Generic
	o := order.Asc
	if sortKey, ok := b.Pool().SortKeys.Primary(); ok {
		o = sortKey.Order
	}
	for _, object := range snap.Select(nil, o) {
		info.Size += object.Size
		if poolSpan == nil {
			poolSpan = extent.NewGenericFromOrder(object.Min, object.Max, order.Asc)
		} else {
			poolSpan.Extend(object.Min)
			poolSpan.Extend(object.Max)
		}
	}
	//XXX need to change API to take return key range
	if poolSpan != nil {
		min := poolSpan.First()
		if min.Type() == super.TypeTime {
			firstTs := super.DecodeTime(min.Bytes())
			lastTs := super.DecodeTime(poolSpan.Last().Bytes())
			if lastTs < firstTs {
				firstTs, lastTs = lastTs, firstTs
			}
			span := nano.NewSpanTs(firstTs, lastTs+1)
			info.Span = &span
		}
	}
	return info, err
}
