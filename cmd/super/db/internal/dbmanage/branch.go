package dbmanage

import (
	"context"

	"github.com/segmentio/ksuid"
	"github.com/superdb/super/api"
	dbapi "github.com/superdb/super/db/api"
	"github.com/superdb/super/db/pools"
	"github.com/superdb/super/dbid"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type branch struct {
	config PoolConfig
	db     dbapi.Interface
	logger *zap.Logger
	pool   *pools.Config
}

func newBranch(c Config, pool *pools.Config, db dbapi.Interface, logger *zap.Logger) *branch {
	config := c.poolConfig(pool)
	logger = logger.Named("pool").With(
		zap.String("name", pool.Name),
		zap.Stringer("id", pool.ID),
		zap.String("branch", config.Branch),
	)
	return &branch{
		config: config,
		db:     db,
		logger: logger,
		pool:   pool,
	}
}

func (b *branch) run(ctx context.Context) error {
	b.logger.Debug("compaction started")
	head := dbid.Committish{Pool: b.pool.Name, Branch: b.config.Branch}
	it, err := newObjectIterator(ctx, b.db, &head)
	if err != nil {
		return err
	}
	defer it.close()
	runCh := make(chan []ksuid.KSUID)
	vecCh := make(chan ksuid.KSUID)
	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error {
		err := scan(ctx, it, b.pool, runCh, vecCh)
		close(runCh)
		close(vecCh)
		return err
	})
	var found int
	var compacted int
	group.Go(func() error {
		for run := range runCh {
			commit, err := b.db.Compact(ctx, b.pool.ID, b.config.Branch, run, api.CommitMessage{})
			if err != nil {
				return err
			}
			found++
			compacted += len(run)
			b.logger.Debug("compacted", zap.Stringer("commit", commit), zap.Int("objects_compacted", len(run)))
		}
		return nil
	})
	err = group.Wait()
	b.logger.Info("compaction completed",
		zap.Int("runs_found", found),
		zap.Int("objects_compacted", compacted),
	)
	return err
}
