// Package dbconn defines Conn, the interface to a SuperDB database used by
// commands, along with helpers that work with any Conn.  Conn is implemented
// both for a local database (see dbconn/local) and for a remote database
// served by the SuperDB service (see dbconn/remote).
package dbconn

import (
	"context"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/vector/vio"
)

// Conn is the interface to a SuperDB database used by commands.  Unlike
// db.Root, which the compiler uses to plan queries against a local database,
// a Conn runs queries and performs operations on the database as a whole.
type Conn interface {
	Query(ctx context.Context, query []srcfiles.Input) (vio.ScanCloser, error)
	TableID(ctx context.Context, tableName string) (uuid.UUID, error)
	CommitObject(ctx context.Context, tableID uuid.UUID, branchName string) (uuid.UUID, error)
	CreateTable(ctx context.Context, name string, sortKeys order.SortKeys, objectCap, frameCap uint64, typ super.Type, sql bool) (uuid.UUID, error)
	RemoveTable(ctx context.Context, tableID uuid.UUID) error
	RenameTable(ctx context.Context, tableID uuid.UUID, name string) error
	CreateBranch(ctx context.Context, tableID uuid.UUID, name string, parent uuid.UUID) error
	RemoveBranch(ctx context.Context, tableID uuid.UUID, branchName string) error
	MergeBranch(ctx context.Context, tableID uuid.UUID, childBranch, parentBranch string, message CommitMessage) (uuid.UUID, error)
	Compact(ctx context.Context, tableID uuid.UUID, branch string, objects []uuid.UUID, message CommitMessage) (uuid.UUID, error)
	Load(ctx context.Context, sctx *super.Context, tableID uuid.UUID, branch string, r vio.Puller, message CommitMessage) (uuid.UUID, error)
	Delete(ctx context.Context, tableID uuid.UUID, branchName string, tags []uuid.UUID, message CommitMessage) (uuid.UUID, error)
	DeleteWhere(ctx context.Context, tableID uuid.UUID, branchName, src string, message CommitMessage) (uuid.UUID, error)
	Revert(ctx context.Context, tableID uuid.UUID, branch string, commitID uuid.UUID, message CommitMessage) (uuid.UUID, error)
	Vacate(ctx context.Context, table string, time nano.Ts, dryrun bool) ([]uuid.UUID, error)
	Vacuum(ctx context.Context, table, revision string, dryrun bool) ([]uuid.UUID, error)
}

// CommitMessage is the author, message body, and metadata of a commit.
type CommitMessage struct {
	Author string `super:"author"`
	Body   string `super:"body"`
	Meta   string `super:"meta"`
}
