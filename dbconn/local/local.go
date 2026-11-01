// Package local implements dbconn.Conn for a database on local storage.
package local

import (
	"context"
	"errors"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/db"
	"github.com/superdb/super/dbconn"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/vector/vio"
	"go.uber.org/zap"
)

type conn struct {
	db       *db.Root
	compiler runtime.Compiler
}

var _ dbconn.Conn = (*conn)(nil)

// Open opens the database at path.
func Open(ctx context.Context, logger *zap.Logger, path string) (dbconn.Conn, error) {
	root, err := OpenDirect(ctx, logger, path)
	if err != nil {
		return nil, err
	}
	return FromRoot(root), nil
}

func OpenDirect(ctx context.Context, logger *zap.Logger, path string) (*db.Root, error) {
	uri, err := storage.ParseURI(path)
	if err != nil {
		return nil, err
	}
	engine := storage.NewLocalEngine()
	root, err := db.Open(ctx, engine, logger, uri)
	if err != nil {
		return nil, err
	}
	return root, nil
}

// Create creates a database at path.
func Create(ctx context.Context, logger *zap.Logger, path string) (dbconn.Conn, error) {
	uri, err := storage.ParseURI(path)
	if err != nil {
		return nil, err
	}
	engine := storage.NewLocalEngine()
	root, err := db.Create(ctx, engine, logger, uri)
	if err != nil {
		return nil, err
	}
	return FromRoot(root), nil
}

func FromRoot(d *db.Root) dbconn.Conn {
	return &conn{db: d, compiler: compiler.NewCompilerForDB(d)}
}

func (l *conn) Root() *db.Root {
	return l.db
}

func (l *conn) CreateTable(ctx context.Context, name string, sortKeys order.SortKeys, objectCap, frameCap uint64, typ super.Type, sql bool) (uuid.UUID, error) {
	if name == "" {
		return uuid.Nil(), errors.New("no table name provided")
	}
	table, err := l.db.CreateTable(ctx, name, sortKeys, objectCap, frameCap, typ, sql)
	if err != nil {
		return uuid.Nil(), err
	}
	return table.ID, nil
}

func (l *conn) RemoveTable(ctx context.Context, id uuid.UUID) error {
	return l.db.RemoveTable(ctx, id)

}

func (l *conn) RenameTable(ctx context.Context, id uuid.UUID, name string) error {
	if name == "" {
		return errors.New("no table provided")
	}
	return l.db.RenameTable(ctx, id, name)
}

func (l *conn) CreateBranch(ctx context.Context, tableID uuid.UUID, name string, parent uuid.UUID) error {
	_, err := l.db.CreateBranch(ctx, tableID, name, parent)
	return err
}

func (l *conn) RemoveBranch(ctx context.Context, tableID uuid.UUID, branchName string) error {
	return l.db.RemoveBranch(ctx, tableID, branchName)
}

func (l *conn) MergeBranch(ctx context.Context, tableID uuid.UUID, childBranch, parentBranch string, message dbconn.CommitMessage) (uuid.UUID, error) {
	return l.db.MergeBranch(ctx, tableID, childBranch, parentBranch, message.Author, message.Body)
}

func (l *conn) Compact(ctx context.Context, tableID uuid.UUID, branchName string, objects []uuid.UUID, commit dbconn.CommitMessage) (uuid.UUID, error) {
	_, branch, err := l.lookupBranch(ctx, tableID, branchName)
	if err != nil {
		return uuid.Nil(), err
	}
	return branch.Compact(ctx, l.compiler, objects, commit.Author, commit.Body, commit.Meta)
}

func (l *conn) Query(ctx context.Context, inputs []srcfiles.Input) (vio.ScanCloser, error) {
	ast, err := parser.ParseFiles(inputs)
	if err != nil {
		return nil, err
	}
	q, err := runtime.CompileQueryForDB(ctx, super.NewContext(), l.compiler, ast)
	if err != nil {
		return nil, err
	}
	return &nopCloser{q}, nil
}

type nopCloser struct {
	vio.Scanner
}

func (*nopCloser) Close() error {
	return nil
}

func (l *conn) TableID(ctx context.Context, tableName string) (uuid.UUID, error) {
	if tableName == "" {
		return uuid.Nil(), errors.New("no table name provided")
	}
	if id, err := dbid.ParseID(tableName); err == nil {
		if _, err := l.db.OpenTable(ctx, id); err == nil {
			return id, nil
		}
	}
	return l.db.TableID(ctx, tableName)
}

func (l *conn) CommitObject(ctx context.Context, tableID uuid.UUID, branchName string) (uuid.UUID, error) {
	return l.db.CommitObject(ctx, tableID, branchName)
}

func (l *conn) lookupBranch(ctx context.Context, tableID uuid.UUID, branchName string) (*db.Table, *db.Branch, error) {
	t, err := l.db.OpenTable(ctx, tableID)
	if err != nil {
		return nil, nil, err
	}
	branch, err := t.OpenBranchByName(ctx, branchName)
	if err != nil {
		return nil, nil, err
	}
	return t, branch, nil
}

func (l *conn) Load(ctx context.Context, ztcx *super.Context, tableID uuid.UUID, branchName string, r vio.Puller, message dbconn.CommitMessage) (uuid.UUID, error) {
	_, branch, err := l.lookupBranch(ctx, tableID, branchName)
	if err != nil {
		return uuid.Nil(), err
	}
	return branch.Load(ctx, ztcx, r, message.Author, message.Body, message.Meta)
}

func (l *conn) Delete(ctx context.Context, tableID uuid.UUID, branchName string, ids []uuid.UUID, message dbconn.CommitMessage) (uuid.UUID, error) {
	_, branch, err := l.lookupBranch(ctx, tableID, branchName)
	if err != nil {
		return uuid.Nil(), err
	}
	commitID, err := branch.Delete(ctx, ids, message.Author, message.Body)
	if err != nil {
		return uuid.Nil(), err
	}
	return commitID, nil
}

func (l *conn) DeleteWhere(ctx context.Context, tableID uuid.UUID, branchName, src string, commit dbconn.CommitMessage) (uuid.UUID, error) {
	ast, err := parser.ParseFiles(srcfiles.Plain(src))
	if err != nil {
		return uuid.Nil(), err
	}
	_, branch, err := l.lookupBranch(ctx, tableID, branchName)
	if err != nil {
		return uuid.Nil(), err
	}
	return branch.DeleteWhere(ctx, l.compiler, ast, commit.Author, commit.Body, commit.Meta)
}

func (l *conn) Revert(ctx context.Context, tableID uuid.UUID, branchName string, commitID uuid.UUID, message dbconn.CommitMessage) (uuid.UUID, error) {
	return l.db.Revert(ctx, tableID, branchName, commitID, message.Author, message.Body)
}

func (l *conn) Vacate(ctx context.Context, tableName string, ts nano.Ts, dryrun bool) ([]uuid.UUID, error) {
	tableID, err := l.TableID(ctx, tableName)
	if err != nil {
		return nil, err
	}
	t, err := l.db.OpenTable(ctx, tableID)
	if err != nil {
		return nil, err
	}
	return t.Vacate(ctx, ts, dryrun)
}

func (l *conn) Vacuum(ctx context.Context, tableName, revision string, dryrun bool) ([]uuid.UUID, error) {
	tableID, err := l.TableID(ctx, tableName)
	if err != nil {
		return nil, err
	}
	t, err := l.db.OpenTable(ctx, tableID)
	if err != nil {
		return nil, err
	}
	commit, err := t.ResolveRevision(ctx, revision)
	if err != nil {
		return nil, err
	}
	return t.Vacuum(ctx, commit, dryrun)
}
