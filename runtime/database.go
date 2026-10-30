package runtime

import (
	"context"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/api"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/vector/vio"
)

// Database is the interface to a SuperDB database used by commands, which
// is implemented both locally (see db/api) and remotely by a service
// connection (see api/client).  Unlike db.Root, which the compiler uses to
// plan queries against a local database, a Database runs queries and
// performs operations on the database as a whole.
type Database interface {
	Query(ctx context.Context, query []srcfiles.Input) (vio.ScanCloser, error)
	TableID(ctx context.Context, tableName string) (uuid.UUID, error)
	CommitObject(ctx context.Context, tableID uuid.UUID, branchName string) (uuid.UUID, error)
	CreateTable(ctx context.Context, name string, sortKeys order.SortKeys, objectCap, frameCap uint64, typ super.Type, sql bool) (uuid.UUID, error)
	RemoveTable(ctx context.Context, tableID uuid.UUID) error
	RenameTable(ctx context.Context, tableID uuid.UUID, name string) error
	CreateBranch(ctx context.Context, tableID uuid.UUID, name string, parent uuid.UUID) error
	RemoveBranch(ctx context.Context, tableID uuid.UUID, branchName string) error
	MergeBranch(ctx context.Context, tableID uuid.UUID, childBranch, parentBranch string, message api.CommitMessage) (uuid.UUID, error)
	Compact(ctx context.Context, tableID uuid.UUID, branch string, objects []uuid.UUID, message api.CommitMessage) (uuid.UUID, error)
	Load(ctx context.Context, sctx *super.Context, tableID uuid.UUID, branch string, r vio.Puller, message api.CommitMessage) (uuid.UUID, error)
	Delete(ctx context.Context, tableID uuid.UUID, branchName string, tags []uuid.UUID, message api.CommitMessage) (uuid.UUID, error)
	DeleteWhere(ctx context.Context, tableID uuid.UUID, branchName, src string, message api.CommitMessage) (uuid.UUID, error)
	Revert(ctx context.Context, tableID uuid.UUID, branch string, commitID uuid.UUID, message api.CommitMessage) (uuid.UUID, error)
	Vacate(ctx context.Context, table string, time nano.Ts, dryrun bool) ([]uuid.UUID, error)
	Vacuum(ctx context.Context, table, revision string, dryrun bool) ([]uuid.UUID, error)
}
