package dbmanage

import (
	"context"
	"uuid"

	"github.com/superdb/super/api"
	dbapi "github.com/superdb/super/db/api"
	"github.com/superdb/super/db/tables"
	"github.com/superdb/super/dbid"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type branch struct {
	config TableConfig
	db     dbapi.Interface
	logger *zap.Logger
	table  *tables.Config
}

func newBranch(c Config, table *tables.Config, db dbapi.Interface, logger *zap.Logger) *branch {
	config := c.tableConfig(table)
	logger = logger.Named("table").With(
		zap.String("name", table.Name),
		zap.Stringer("id", table.ID),
		zap.String("branch", config.Branch),
	)
	return &branch{
		config: config,
		db:     db,
		logger: logger,
		table:  table,
	}
}

func (b *branch) run(ctx context.Context) error {
	b.logger.Debug("compaction started")
	head := dbid.Committish{Table: b.table.Name, Branch: b.config.Branch}
	it, err := newObjectIterator(ctx, b.db, &head)
	if err != nil {
		return err
	}
	defer it.close()
	runCh := make(chan []uuid.UUID)
	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error {
		err := scan(ctx, it, b.table, runCh)
		close(runCh)
		return err
	})
	var found int
	var compacted int
	group.Go(func() error {
		for run := range runCh {
			commit, err := b.db.Compact(ctx, b.table.ID, b.config.Branch, run, api.CommitMessage{})
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
