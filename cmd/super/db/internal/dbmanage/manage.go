package dbmanage

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"
	"uuid"

	"github.com/superdb/super/api/client"
	"github.com/superdb/super/db/api"
	"github.com/superdb/super/db/pools"
	"github.com/superdb/super/dbid"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

func Update(ctx context.Context, db api.Interface, conf Config, logger *zap.Logger) error {
	if logger == nil {
		logger = zap.NewNop()
	}
	branches, err := getBranches(ctx, conf, db, logger)
	if err != nil {
		return err
	}
	group, ctx := errgroup.WithContext(ctx)
	for _, branch := range branches {
		branch.logger.Info("updating pool")
		if err := branch.run(ctx); err != nil {
			branch.logger.Error("update error", zap.Error(err))
		}
	}
	return group.Wait()
}

func Monitor(ctx context.Context, conn *client.Connection, conf Config, logger *zap.Logger) error {
	if logger == nil {
		logger = zap.NewNop()
	}
	logger.Info("monitoring")
	db := api.NewRemoteDB(conn)
	for {
		err := monitor(ctx, db, conf, logger)
		if errors.Is(err, syscall.ECONNREFUSED) {
			logger.Info("cannot connect to database, retrying in 5 seconds")
		} else if err != nil {
			return err
		}
		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func monitor(ctx context.Context, db api.Interface, conf Config, logger *zap.Logger) error {
	for {
		select {
		case <-time.After(conf.interval()):
		case <-ctx.Done():
			return ctx.Err()
		}
		err := Update(ctx, db, conf, logger)
		if err != nil {
			return err
		}
	}
}

func getBranches(ctx context.Context, conf Config, db api.Interface, logger *zap.Logger) ([]*branch, error) {
	pools, err := getPools(ctx, conf, db)
	if err != nil {
		return nil, err
	}
	var branches []*branch
	for _, pool := range pools {
		if b := newBranch(conf, pool, db, logger); b != nil {
			branches = append(branches, b)
		}
	}
	return branches, nil
}

func getPools(ctx context.Context, conf Config, db api.Interface) ([]*pools.Config, error) {
	pls, err := api.GetPools(ctx, db)
	if err != nil {
		return nil, err
	}
	if len(conf.Pools) == 0 {
		return pls, nil
	}
	m := map[uuid.UUID]struct{}{}
	var out []*pools.Config
	for _, c := range conf.Pools {
		p := selectPool(c, pls)
		if p == nil {
			return nil, fmt.Errorf("pool %q not found", c.Pool)
		}
		if _, ok := m[p.ID]; ok {
			return nil, fmt.Errorf("duplicate pool in configuration: %q", c.Pool)
		}
		m[p.ID] = struct{}{}
		out = append(out, p)
	}
	return out, nil
}

func selectPool(c PoolConfig, pools []*pools.Config) *pools.Config {
	id, _ := dbid.ParseID(c.Pool)
	for _, p := range pools {
		if id == p.ID || c.Pool == p.Name {
			return p
		}
	}
	return nil
}
