package api

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/segmentio/ksuid"
	"github.com/superdb/super"
	"github.com/superdb/super/api"
	"github.com/superdb/super/api/client"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/db"
	"github.com/superdb/super/db/commits"
	"github.com/superdb/super/db/pools"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
	"go.uber.org/zap"
)

type Interface interface {
	Root() *db.Root
	Query(ctx context.Context, query []srcfiles.Input) (vio.Scanner, error)
	PoolID(ctx context.Context, poolName string) (ksuid.KSUID, error)
	CommitObject(ctx context.Context, poolID ksuid.KSUID, branchName string) (ksuid.KSUID, error)
	CreatePool(context.Context, string, order.SortKeys, int64) (ksuid.KSUID, error)
	RemovePool(context.Context, ksuid.KSUID) error
	RenamePool(context.Context, ksuid.KSUID, string) error
	CreateBranch(ctx context.Context, pool ksuid.KSUID, name string, parent ksuid.KSUID) error
	RemoveBranch(ctx context.Context, pool ksuid.KSUID, branchName string) error
	MergeBranch(ctx context.Context, pool ksuid.KSUID, childBranch, parentBranch string, message api.CommitMessage) (ksuid.KSUID, error)
	Compact(ctx context.Context, pool ksuid.KSUID, branch string, objects []ksuid.KSUID, message api.CommitMessage) (ksuid.KSUID, error)
	Load(ctx context.Context, sctx *super.Context, pool ksuid.KSUID, branch string, r sio.Reader, message api.CommitMessage) (ksuid.KSUID, error)
	Delete(ctx context.Context, poolID ksuid.KSUID, branchName string, tags []ksuid.KSUID, message api.CommitMessage) (ksuid.KSUID, error)
	DeleteWhere(ctx context.Context, poolID ksuid.KSUID, branchName, src string, commit api.CommitMessage) (ksuid.KSUID, error)
	Revert(ctx context.Context, poolID ksuid.KSUID, branch string, commitID ksuid.KSUID, commit api.CommitMessage) (ksuid.KSUID, error)
	Vacate(ctx context.Context, pool string, time nano.Ts, dryrun bool) ([]ksuid.KSUID, error)
	Vacuum(ctx context.Context, pool, revision string, dryrun bool) ([]ksuid.KSUID, error)
}

func Connect(ctx context.Context, logger *zap.Logger, u string) (Interface, error) {
	if IsRemote(u) {
		return NewRemoteDB(client.NewConnectionTo(u)), nil
	}
	return OpenLocalDB(ctx, logger, u)
}

func IsRemote(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

func LookupPoolByName(ctx context.Context, api Interface, name string) (*pools.Config, error) {
	b := newBuffer(pools.Config{})
	query := fmt.Sprintf("from :pools | name == '%s'", name)
	q, err := api.Query(ctx, srcfiles.Plain(query))
	if err != nil {
		return nil, err
	}
	defer q.Pull(true)
	if err := vio.Copy(b, q); err != nil {
		return nil, err
	}
	switch len(b.results) {
	case 0:
		return nil, fmt.Errorf("%q: pool not found", name)
	case 1:
		pool, ok := b.results[0].(*pools.Config)
		if !ok {
			return nil, fmt.Errorf("internal error: pool record has wrong type: %T", b.results[0])
		}
		return pool, nil
	default:
		return nil, fmt.Errorf("internal error: multiple pools found with same name: %s", name)
	}
}

func GetPools(ctx context.Context, api Interface) ([]*pools.Config, error) {
	b := newBuffer(pools.Config{})
	q, err := api.Query(ctx, srcfiles.Plain("from :pools"))
	if err != nil {
		return nil, err
	}
	defer q.Pull(true)
	if err := vio.Copy(b, q); err != nil {
		return nil, err
	}
	var pls []*pools.Config
	for _, r := range b.results {
		pls = append(pls, r.(*pools.Config))
	}
	return pls, nil
}

func LookupPoolByID(ctx context.Context, api Interface, id ksuid.KSUID) (*pools.Config, error) {
	b := newBuffer(pools.Config{})
	query := fmt.Sprintf("from :pools | id == hex('%s')", idToHex(id))
	q, err := api.Query(ctx, srcfiles.Plain(query))
	if err != nil {
		return nil, err
	}
	defer q.Pull(true)
	if err := vio.Copy(b, q); err != nil {
		return nil, err
	}
	switch len(b.results) {
	case 0:
		return nil, fmt.Errorf("%s: pool not found", id)
	case 1:
		pool, ok := b.results[0].(*pools.Config)
		if !ok {
			return nil, fmt.Errorf("internal error: pool record has wrong type: %T", b.results[0])
		}
		return pool, nil
	default:
		return nil, fmt.Errorf("internal error: multiple pools found with same id: %s", id)
	}
}

func LookupBranchByName(ctx context.Context, api Interface, poolName, branchName string) (*db.BranchMeta, error) {
	b := newBuffer(db.BranchMeta{})
	query := fmt.Sprintf("from :branches | pool.name == '%s' branch.name == '%s'", poolName, branchName)
	q, err := api.Query(ctx, srcfiles.Plain(query))
	if err != nil {
		return nil, err
	}
	defer q.Pull(true)
	if err := vio.Copy(b, q); err != nil {
		return nil, err
	}
	switch len(b.results) {
	case 0:
		return nil, fmt.Errorf("%q: branch not found", poolName+"/"+branchName)
	case 1:
		branch, ok := b.results[0].(*db.BranchMeta)
		if !ok {
			return nil, fmt.Errorf("internal error: branch record has wrong type: %T", b.results[0])
		}
		return branch, nil
	default:
		return nil, fmt.Errorf("internal error: multiple branches found with same name: %s", poolName+"/"+branchName)
	}
}

func LookupBranchByID(ctx context.Context, api Interface, id ksuid.KSUID) (*db.BranchMeta, error) {
	b := newBuffer(db.BranchMeta{})
	query := fmt.Sprintf("from :branches | branch.id == 'hex(%s)'", idToHex(id))
	q, err := api.Query(ctx, srcfiles.Plain(query))
	if err != nil {
		return nil, err
	}
	defer q.Pull(true)
	if err := vio.Copy(b, q); err != nil {
		return nil, err
	}
	switch len(b.results) {
	case 0:
		return nil, fmt.Errorf("%s: branch not found", id)
	case 1:
		branch, ok := b.results[0].(*db.BranchMeta)
		if !ok {
			return nil, fmt.Errorf("internal error: branch record has wrong type: %T", b.results[0])
		}
		return branch, nil
	default:
		return nil, fmt.Errorf("internal error: multiple branches found with same id: %s", id)
	}
}

func GetCommit(ctx context.Context, api Interface, pool, revision string) (*commits.Commit, error) {
	poolID, err := api.PoolID(ctx, pool)
	if err != nil {
		return nil, err
	}
	commit, err := dbid.ParseID(revision)
	if err != nil {
		// LookupBranchByName(ctx
		if commit, err = api.CommitObject(ctx, poolID, revision); err != nil {
			return nil, err
		}
	}
	b := newBuffer(commits.Commit{})
	query := fmt.Sprintf("from %q:log | where id == 0x%s", poolID, idToHex(commit))
	q, err := api.Query(ctx, srcfiles.Plain(query))
	if err != nil {
		return nil, err
	}
	defer q.Pull(true)
	if err := vio.Copy(b, q); err != nil {
		return nil, err
	}
	switch len(b.results) {
	case 0:
		return nil, fmt.Errorf("%s: commit not found", commit)
	case 1:
		commit, ok := b.results[0].(*commits.Commit)
		if !ok {
			return nil, fmt.Errorf("internal error: branch record has wrong type: %T", b.results[0])
		}
		return commit, nil
	default:
		return nil, fmt.Errorf("internal error: multiple commits found with same id: %s", commit)
	}
}

func idToHex(id ksuid.KSUID) string {
	return hex.EncodeToString(id.Bytes())
}

type buffer struct {
	unmarshaler *super.Unmarshaler
	results     []any
}

var _ sio.Writer = (*buffer)(nil)

func newBuffer(types ...any) *buffer {
	u := super.NewUnmarshaler()
	u.Bind(types...)
	return &buffer{unmarshaler: u}
}

func (b *buffer) Push(vec vector.Any) error {
	return sbuf.WriteVec(b, vec)
}

func (b *buffer) Write(val super.Value) error {
	var v any
	if err := b.unmarshaler.Unmarshal(val, &v); err != nil {
		return err
	}
	b.results = append(b.results, v)
	return nil
}
