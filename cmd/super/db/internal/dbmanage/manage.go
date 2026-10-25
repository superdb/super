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
	"github.com/superdb/super/db/tables"
	"github.com/superdb/super/dbid"
	"go.uber.org/zap"
)

// Update runs one maintenance pass over the configured tables.  A failure on
// one table doesn't stop the others; all failures are returned together.
func Update(ctx context.Context, db api.Interface, conf Config, logger *zap.Logger) error {
	if logger == nil {
		logger = zap.NewNop()
	}
	branches, err := getBranches(ctx, conf, db, logger)
	if err != nil {
		return err
	}
	var errs []error
	for _, branch := range branches {
		branch.logger.Info("updating table")
		if err := branch.run(ctx); err != nil {
			branch.logger.Error("update error", zap.Error(err))
			errs = append(errs, fmt.Errorf("table %q: %w", branch.table.Name, err))
		}
	}
	return errors.Join(errs...)
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
		if err := Update(ctx, db, conf, logger); err != nil {
			// Let Monitor handle a lost connection.  Otherwise keep
			// monitoring since a later pass may succeed (e.g., after a
			// conflicting commit); Update has already logged the error.
			if errors.Is(err, syscall.ECONNREFUSED) || ctx.Err() != nil {
				return err
			}
		}
	}
}

func getBranches(ctx context.Context, conf Config, db api.Interface, logger *zap.Logger) ([]*branch, error) {
	tables, err := getTables(ctx, conf, db)
	if err != nil {
		return nil, err
	}
	var branches []*branch
	for _, table := range tables {
		if b := newBranch(conf, table, db, logger); b != nil {
			branches = append(branches, b)
		}
	}
	return branches, nil
}

func getTables(ctx context.Context, conf Config, db api.Interface) ([]*tables.Config, error) {
	tabs, err := api.GetTables(ctx, db)
	if err != nil {
		return nil, err
	}
	if len(conf.Tables) == 0 {
		return tabs, nil
	}
	m := map[uuid.UUID]struct{}{}
	var out []*tables.Config
	for _, c := range conf.Tables {
		p := selectTable(c, tabs)
		if p == nil {
			return nil, fmt.Errorf("table %q not found", c.Table)
		}
		if _, ok := m[p.ID]; ok {
			return nil, fmt.Errorf("duplicate table in configuration: %q", c.Table)
		}
		m[p.ID] = struct{}{}
		out = append(out, p)
	}
	return out, nil
}

func selectTable(c TableConfig, tables []*tables.Config) *tables.Config {
	id, _ := dbid.ParseID(c.Table)
	for _, t := range tables {
		if id == t.ID || c.Table == t.Name {
			return t
		}
	}
	return nil
}
