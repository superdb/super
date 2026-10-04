package api

import (
	"context"
	"errors"

	"github.com/segmentio/ksuid"
	"github.com/superdb/super"
	"github.com/superdb/super/api"
	"github.com/superdb/super/compiler"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/db"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/vector/vio"
	"go.uber.org/zap"
)

type local struct {
	db       *db.Root
	compiler runtime.Compiler
}

var _ Interface = (*local)(nil)

func OpenLocalDB(ctx context.Context, logger *zap.Logger, path string) (Interface, error) {
	uri, err := storage.ParseURI(path)
	if err != nil {
		return nil, err
	}
	engine := storage.NewLocalEngine()
	root, err := db.Open(ctx, engine, logger, uri)
	if err != nil {
		return nil, err
	}
	return FromRoot(root), nil
}

func CreateLocalDB(ctx context.Context, logger *zap.Logger, path string) (Interface, error) {
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

func FromRoot(d *db.Root) Interface {
	return &local{db: d, compiler: compiler.NewCompilerForDB(d)}
}

func (l *local) Root() *db.Root {
	return l.db
}

func (l *local) CreatePool(ctx context.Context, name string, sortKeys order.SortKeys, thresh int64) (ksuid.KSUID, error) {
	if name == "" {
		return ksuid.Nil, errors.New("no pool name provided")
	}
	pool, err := l.db.CreatePool(ctx, name, sortKeys, thresh)
	if err != nil {
		return ksuid.Nil, err
	}
	return pool.ID, nil
}

func (l *local) RemovePool(ctx context.Context, id ksuid.KSUID) error {
	return l.db.RemovePool(ctx, id)

}

func (l *local) RenamePool(ctx context.Context, id ksuid.KSUID, name string) error {
	if name == "" {
		return errors.New("no pool name provided")
	}
	return l.db.RenamePool(ctx, id, name)
}

func (l *local) CreateBranch(ctx context.Context, poolID ksuid.KSUID, name string, parent ksuid.KSUID) error {
	_, err := l.db.CreateBranch(ctx, poolID, name, parent)
	return err
}

func (l *local) RemoveBranch(ctx context.Context, poolID ksuid.KSUID, branchName string) error {
	return l.db.RemoveBranch(ctx, poolID, branchName)
}

func (l *local) MergeBranch(ctx context.Context, poolID ksuid.KSUID, childBranch, parentBranch string, message api.CommitMessage) (ksuid.KSUID, error) {
	return l.db.MergeBranch(ctx, poolID, childBranch, parentBranch, message.Author, message.Body)
}

func (l *local) Compact(ctx context.Context, poolID ksuid.KSUID, branchName string, objects []ksuid.KSUID, commit api.CommitMessage) (ksuid.KSUID, error) {
	pool, err := l.db.OpenPool(ctx, poolID)
	if err != nil {
		return ksuid.Nil, err
	}
	return exec.Compact(ctx, l.db, pool, branchName, objects, commit.Author, commit.Body, commit.Meta)
}

func (l *local) Query(ctx context.Context, inputs []srcfiles.Input) (vio.Scanner, error) {
	ast, err := parser.ParseFiles(inputs)
	if err != nil {
		return nil, err
	}
	q, err := runtime.CompileQueryForDB(ctx, super.NewContext(), l.compiler, ast)
	if err != nil {
		return nil, err
	}
	return q, nil
}

func (l *local) PoolID(ctx context.Context, poolName string) (ksuid.KSUID, error) {
	if poolName == "" {
		return ksuid.Nil, errors.New("no pool name provided")
	}
	if id, err := dbid.ParseID(poolName); err == nil {
		if _, err := l.db.OpenPool(ctx, id); err == nil {
			return id, nil
		}
	}
	return l.db.PoolID(ctx, poolName)
}

func (l *local) CommitObject(ctx context.Context, poolID ksuid.KSUID, branchName string) (ksuid.KSUID, error) {
	return l.db.CommitObject(ctx, poolID, branchName)
}

func (l *local) lookupBranch(ctx context.Context, poolID ksuid.KSUID, branchName string) (*db.Pool, *db.Branch, error) {
	pool, err := l.db.OpenPool(ctx, poolID)
	if err != nil {
		return nil, nil, err
	}
	branch, err := pool.OpenBranchByName(ctx, branchName)
	if err != nil {
		return nil, nil, err
	}
	return pool, branch, nil
}

func (l *local) Load(ctx context.Context, ztcx *super.Context, poolID ksuid.KSUID, branchName string, r sio.Reader, message api.CommitMessage) (ksuid.KSUID, error) {
	_, branch, err := l.lookupBranch(ctx, poolID, branchName)
	if err != nil {
		return ksuid.Nil, err
	}
	return branch.Load(ctx, ztcx, r, message.Author, message.Body, message.Meta)
}

func (l *local) Delete(ctx context.Context, poolID ksuid.KSUID, branchName string, ids []ksuid.KSUID, message api.CommitMessage) (ksuid.KSUID, error) {
	_, branch, err := l.lookupBranch(ctx, poolID, branchName)
	if err != nil {
		return ksuid.Nil, err
	}
	commitID, err := branch.Delete(ctx, ids, message.Author, message.Body)
	if err != nil {
		return ksuid.Nil, err
	}
	return commitID, nil
}

func (l *local) DeleteWhere(ctx context.Context, poolID ksuid.KSUID, branchName, src string, commit api.CommitMessage) (ksuid.KSUID, error) {
	ast, err := parser.ParseFiles(srcfiles.Plain(src))
	if err != nil {
		return ksuid.Nil, err
	}
	_, branch, err := l.lookupBranch(ctx, poolID, branchName)
	if err != nil {
		return ksuid.Nil, err
	}
	return branch.DeleteWhere(ctx, l.compiler, ast, commit.Author, commit.Body, commit.Meta)
}

func (l *local) Revert(ctx context.Context, poolID ksuid.KSUID, branchName string, commitID ksuid.KSUID, message api.CommitMessage) (ksuid.KSUID, error) {
	return l.db.Revert(ctx, poolID, branchName, commitID, message.Author, message.Body)
}

func (l *local) Vacate(ctx context.Context, pool string, ts nano.Ts, dryrun bool) ([]ksuid.KSUID, error) {
	poolID, err := l.PoolID(ctx, pool)
	if err != nil {
		return nil, err
	}
	p, err := l.db.OpenPool(ctx, poolID)
	if err != nil {
		return nil, err
	}
	return p.Vacate(ctx, ts, dryrun)
}

func (l *local) Vacuum(ctx context.Context, pool, revision string, dryrun bool) ([]ksuid.KSUID, error) {
	poolID, err := l.PoolID(ctx, pool)
	if err != nil {
		return nil, err
	}
	p, err := l.db.OpenPool(ctx, poolID)
	if err != nil {
		return nil, err
	}
	commit, err := p.ResolveRevision(ctx, revision)
	if err != nil {
		return nil, err
	}
	return p.Vacuum(ctx, commit, dryrun)
}
