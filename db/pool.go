package db

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/segmentio/ksuid"
	"github.com/superdb/super"
	"github.com/superdb/super/db/branches"
	"github.com/superdb/super/db/commits"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/db/pools"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/bsupio"
	"github.com/superdb/super/vector/vio"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	DataTag     = "data"
	BranchesTag = "branches"
	CommitsTag  = "commits"
)

type Pool struct {
	pools.Config
	engine   storage.Engine
	Path     *storage.URI
	DataPath *storage.URI
	branches *branches.Store
	commits  *commits.Store
}

func CreatePool(ctx context.Context, engine storage.Engine, logger *zap.Logger, root *storage.URI, config *pools.Config) error {
	poolPath := config.Path(root)
	// branchesPath is the path to the kvs journal of BranchConfigs
	// for the pool while the commit log is stored in <pool-id>/<branch-id>.
	branchesPath := poolPath.JoinPath(BranchesTag)
	// create the branches journal store
	_, err := branches.CreateStore(ctx, engine, logger, branchesPath)
	if err != nil {
		return err
	}
	// create the main branch in the branches journal store.  The parent
	// commit object of the initial main branch is ksuid.Nil.
	_, err = CreateBranch(ctx, engine, logger, root, config, "main", ksuid.Nil)
	return err
}

func CreateBranch(ctx context.Context, engine storage.Engine, logger *zap.Logger, root *storage.URI, poolConfig *pools.Config, name string, parent ksuid.KSUID) (*branches.Config, error) {
	poolPath := poolConfig.Path(root)
	branchesPath := poolPath.JoinPath(BranchesTag)
	store, err := branches.OpenStore(ctx, engine, logger, branchesPath)
	if err != nil {
		return nil, err
	}
	if _, err := store.LookupByName(ctx, name); err == nil {
		return nil, fmt.Errorf("%s/%s: %w", poolConfig.Name, name, branches.ErrExists)
	}
	branchConfig := branches.NewConfig(name, parent)
	if err := store.Add(ctx, branchConfig); err != nil {
		return nil, err
	}
	return branchConfig, err
}

func OpenPool(ctx context.Context, engine storage.Engine, logger *zap.Logger, root *storage.URI, config *pools.Config) (*Pool, error) {
	path := config.Path(root)
	branchesPath := path.JoinPath(BranchesTag)
	branches, err := branches.OpenStore(ctx, engine, logger, branchesPath)
	if err != nil {
		return nil, err
	}
	commitsPath := path.JoinPath(CommitsTag)
	commits, err := commits.OpenStore(engine, logger, commitsPath)
	if err != nil {
		return nil, err
	}
	return &Pool{
		Config:   *config,
		engine:   engine,
		Path:     path,
		DataPath: DataPath(path),
		branches: branches,
		commits:  commits,
	}, nil
}

func RemovePool(ctx context.Context, engine storage.Engine, root *storage.URI, config *pools.Config) error {
	return engine.DeleteByPrefix(ctx, config.Path(root))
}

func (p *Pool) removeBranch(ctx context.Context, name string) error {
	config, err := p.branches.LookupByName(ctx, name)
	if err != nil {
		return err
	}
	return p.branches.Remove(ctx, *config)
}

func (p *Pool) Snapshot(ctx context.Context, commit ksuid.KSUID) (commits.View, error) {
	return p.commits.Snapshot(ctx, commit)
}

func (p *Pool) OpenCommitLog(ctx context.Context, sctx *super.Context, commit ksuid.KSUID) sio.Reader {
	return p.commits.OpenCommitLog(ctx, sctx, commit, ksuid.Nil)
}

func (p *Pool) OpenCommitLogAsBSUP(ctx context.Context, sctx *super.Context, commit ksuid.KSUID) (sio.Reader, error) {
	return p.commits.OpenAsBSUP(ctx, sctx, commit, ksuid.Nil)
}

func (p *Pool) Storage() storage.Engine {
	return p.engine
}

func (p *Pool) ListBranches(ctx context.Context) ([]branches.Config, error) {
	return p.branches.All(ctx)
}

func (p *Pool) LookupBranchByName(ctx context.Context, name string) (*branches.Config, error) {
	return p.branches.LookupByName(ctx, name)
}

func (p *Pool) openBranch(ctx context.Context, config *branches.Config) (*Branch, error) {
	return OpenBranch(ctx, config, p.engine, p.Path, p)
}

func (p *Pool) OpenBranchByName(ctx context.Context, name string) (*Branch, error) {
	branchRef, err := p.LookupBranchByName(ctx, name)
	if err != nil {
		return nil, err
	}
	return p.openBranch(ctx, branchRef)
}

// ResolveRevision returns the commit id for revision. revision can be either a
// commit ID in string form or a branch name.
func (p *Pool) ResolveRevision(ctx context.Context, revision string) (ksuid.KSUID, error) {
	id, err := dbid.ParseID(revision)
	if err != nil {
		branch, err := p.LookupBranchByName(ctx, revision)
		if err != nil {
			return ksuid.Nil, err
		}
		id = branch.Commit
	}
	return id, nil
}

func (p *Pool) BatchifyBranches(ctx context.Context, sctx *super.Context, recs []super.Value, m *super.Marshaler, f expr.Evaluator) ([]super.Value, error) {
	branches, err := p.ListBranches(ctx)
	if err != nil {
		return nil, err
	}
	for _, branchRef := range branches {
		meta := BranchMeta{p.Config, branchRef}
		rec, err := m.Marshal(&meta)
		if err != nil {
			return nil, err
		}
		if filter(sctx, rec, f) {
			recs = append(recs, rec)
		}
	}
	return recs, nil
}

func filter(sctx *super.Context, this super.Value, e expr.Evaluator) bool {
	if e == nil {
		return true
	}
	return expr.EvalBool(sctx, this, e).Ptr().AsBool()
}

type BranchTip struct {
	Name   string
	Commit ksuid.KSUID
}

func (p *Pool) BatchifyBranchTips(ctx context.Context, sctx *super.Context, f expr.Evaluator) ([]super.Value, error) {
	branches, err := p.ListBranches(ctx)
	if err != nil {
		return nil, err
	}
	m := super.NewMarshaler(sctx)
	m.Decorate(super.StylePackage)
	recs := make([]super.Value, 0, len(branches))
	for _, branchRef := range branches {
		rec, err := m.Marshal(&BranchTip{branchRef.Name, branchRef.Commit})
		if err != nil {
			return nil, err
		}
		if filter(sctx, rec, f) {
			recs = append(recs, rec)
		}
	}
	return recs, nil
}

// XXX this is inefficient but is only meant for interactive queries...?
func (p *Pool) ObjectExists(ctx context.Context, id ksuid.KSUID) (bool, error) {
	return p.engine.Exists(ctx, data.URI(p.DataPath, id))
}

func (p *Pool) Vacate(ctx context.Context, ts nano.Ts, dryrun bool) ([]ksuid.KSUID, error) {
	if !dryrun {
		if err := p.vacateBranchStore(ctx, ts, dryrun); err != nil {
			return nil, err
		}
	}
	return p.vacateCommits(ctx, ts, dryrun)
}

func (p *Pool) vacateCommits(ctx context.Context, ts nano.Ts, dryrun bool) ([]ksuid.KSUID, error) {
	main, err := p.Main(ctx)
	if err != nil {
		return nil, err
	}
	commit, err := p.commits.FindNearestToTs(ctx, main.Branch.Commit, ts)
	if err != nil {
		return nil, err
	}
	branches, err := p.branches.All(ctx)
	if err != nil {
		return nil, err
	}
	var conflicts []string
	for _, b := range branches {
		// Find any branches whose history does not include commit and if any
		// are found fail.
		path, err := p.commits.Path(ctx, b.Commit)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(path, commit) {
			conflicts = append(conflicts, b.Name)
		}
	}
	if len(conflicts) > 0 {
		slices.Sort(conflicts)
		v := "branch does not include commit nearest timestamp"
		if len(conflicts) > 1 {
			v = "branches do not include commit nearest timestamp"
		}
		return nil, fmt.Errorf("cannot vacate at selected time: %s in the deletion path: %s", v, strings.Join(conflicts, ", "))
	}
	if dryrun {
		path, err := p.commits.Path(ctx, commit)
		if err != nil {
			return nil, err
		}
		return path[1:], nil
	}
	return p.commits.SetBase(ctx, commit)
}

func (p *Pool) vacateBranchStore(ctx context.Context, ts nano.Ts, dryrun bool) error {
	var werr error
	at, err := p.branches.EntryWhere(ctx, func(config *branches.Config) bool {
		if config.Commit.IsNil() {
			return false
		}
		var c *commits.Commit
		_, c, werr = p.commits.GetBytes(ctx, config.Commit)
		return werr != nil || ts >= c.Date
	})
	if err = errors.Join(err, werr); err != nil {
		return err
	}
	return p.branches.TruncateHistory(ctx, at)
}

func (p *Pool) Vacuum(ctx context.Context, commit ksuid.KSUID, dryrun bool) ([]ksuid.KSUID, error) {
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(runtime.GOMAXPROCS(0))
	ch := make(chan *data.Object)
	group.Go(func() error {
		defer close(ch)
		return p.commits.Vacuumable(ctx, commit, ch)
	})
	var vacuumed []ksuid.KSUID
	var mu sync.Mutex
	for o := range ch {
		if dryrun {
			// For dryrun just check if the object exists and append existing
			// objects to list of results.
			group.Go(func() error {
				ok, err := p.engine.Exists(ctx, data.URI(p.DataPath, o.ID))
				if ok {
					mu.Lock()
					vacuumed = append(vacuumed, o.ID)
					mu.Unlock()
				}
				return err
			})
			continue
		}
		group.Go(func() error {
			err := p.engine.Delete(ctx, data.URI(p.DataPath, o.ID))
			if err == nil {
				mu.Lock()
				vacuumed = append(vacuumed, o.ID)
				mu.Unlock()
			}
			if errors.Is(err, fs.ErrNotExist) {
				err = nil
			}
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return vacuumed, nil
}

func (p *Pool) Main(ctx context.Context) (BranchMeta, error) {
	branch, err := p.OpenBranchByName(ctx, "main")
	if err != nil {
		return BranchMeta{}, err
	}
	return BranchMeta{p.Config, branch.Config}, nil
}

// NewReader returns a Reader for this data object. If the object has a seek index
// and if the provided span skips part of the object, the seek index will be used to
// limit the reading window of the returned reader.
func (p *Pool) NewReader(ctx context.Context, sctx *super.Context, object *data.Object, pushdown sbuf.Pushdown) (vio.ScanCloser, error) {
	uri := object.URI(p.DataPath)
	r, err := p.engine.Get(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", uri, err)
	}
	scanner, err := bsupio.NewReader(ctx, sctx, r, pushdown, 1) //XXX concurrency arg
	if err != nil {
		return nil, err
	}
	return &struct {
		vio.Scanner
		io.Closer
	}{
		Scanner: scanner,
		Closer:  r,
	}, nil
}

func DataPath(poolPath *storage.URI) *storage.URI {
	return poolPath.JoinPath(DataTag)
}
