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
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/db/branches"
	"github.com/superdb/super/db/commits"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/db/tables"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/bsupio"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector/vio"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	DataTag     = "data"
	BranchesTag = "branches"
	CommitsTag  = "commits"
)

type Table struct {
	tables.Config
	engine   storage.Engine
	Path     *storage.URI
	DataPath *storage.URI
	branches *branches.Store
	commits  *commits.Store
}

// CreateTable creates the storage for a table.  If config.Type is not nil,
// all values loaded into the table must conform to it (see super.Conformer).
func CreateTable(ctx context.Context, engine storage.Engine, logger *zap.Logger, root *storage.URI, config *tables.Config) error {
	tablePath := config.Path(root)
	// branchesPath is the path to the kvs journal of BranchConfigs
	// for the table while the commit log is stored in <table-id>/<branch-id>.
	branchesPath := tablePath.JoinPath(BranchesTag)
	// create the branches journal store
	_, err := branches.CreateStore(ctx, engine, logger, branchesPath)
	if err != nil {
		return err
	}
	// create the main branch in the branches journal store.  The parent
	// commit object of the initial main branch is uuid.Nil().
	_, err = CreateBranch(ctx, engine, logger, root, config, "main", uuid.Nil())
	return err
}

func CreateBranch(ctx context.Context, engine storage.Engine, logger *zap.Logger, root *storage.URI, tableConfig *tables.Config, name string, parent uuid.UUID) (*branches.Config, error) {
	tablePath := tableConfig.Path(root)
	branchesPath := tablePath.JoinPath(BranchesTag)
	store, err := branches.OpenStore(ctx, engine, logger, branchesPath)
	if err != nil {
		return nil, err
	}
	if _, err := store.LookupByName(ctx, name); err == nil {
		return nil, fmt.Errorf("%s/%s: %w", tableConfig.Name, name, branches.ErrExists)
	}
	branchConfig := branches.NewConfig(name, parent)
	if err := store.Add(ctx, branchConfig); err != nil {
		return nil, err
	}
	return branchConfig, err
}

func OpenTable(ctx context.Context, engine storage.Engine, logger *zap.Logger, root *storage.URI, config *tables.Config) (*Table, error) {
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
	return &Table{
		Config:   *config,
		engine:   engine,
		Path:     path,
		DataPath: DataPath(path),
		branches: branches,
		commits:  commits,
	}, nil
}

// TypeConstraint returns the table's type in sctx or nil if the table has
// no type.
func (t *Table) TypeConstraint(sctx *super.Context) (super.Type, error) {
	if len(t.Config.Type) == 0 {
		return nil, nil
	}
	typ, err := sctx.LookupByValue(t.Config.Type)
	if err != nil {
		// We have a footgun here since we can persist named types
		// and they can conflict with new versions of named types in
		// a query.  We detect this and report the error.  We plan to
		// fix this with UUIDs in named types and type versioning.
		var text string
		if typ, err := super.NewContext().LookupByValue(t.Config.Type); err == nil {
			text = sup.FormatType(typ) + " "
		}
		return nil, fmt.Errorf("table %q: type %sconflicts with a type of the same name: %w", t.Name, text, err)
	}
	return typ, nil
}

func RemoveTable(ctx context.Context, engine storage.Engine, root *storage.URI, config *tables.Config) error {
	return engine.DeleteByPrefix(ctx, config.Path(root))
}

func (t *Table) removeBranch(ctx context.Context, name string) error {
	config, err := t.branches.LookupByName(ctx, name)
	if err != nil {
		return err
	}
	return t.branches.Remove(ctx, *config)
}

func (t *Table) Snapshot(ctx context.Context, commit uuid.UUID) (commits.View, error) {
	return t.commits.Snapshot(ctx, commit)
}

// Type returns the fused type all of the data objects in the snapshot at commit,
// interpreted in sctx.  It returns nil if the type cannot be known statically
// (no commit yet, an empty snapshot, or an object lacking a type).
func (t *Table) Type(ctx context.Context, sctx *super.Context, commit uuid.UUID) (super.Type, error) {
	if commit == uuid.Nil() {
		return nil, nil
	}
	snap, err := t.Snapshot(ctx, commit)
	if err != nil {
		return nil, err
	}
	return data.FusedType(sctx, snap.SelectAll())
}

func (t *Table) OpenCommitLog(ctx context.Context, sctx *super.Context, commit uuid.UUID) sio.Reader {
	return t.commits.OpenCommitLog(ctx, sctx, commit, uuid.Nil())
}

func (t *Table) OpenCommitLogAsBSUP(ctx context.Context, sctx *super.Context, commit uuid.UUID) (sio.Reader, error) {
	return t.commits.OpenAsBSUP(ctx, sctx, commit, uuid.Nil())
}

func (t *Table) Storage() storage.Engine {
	return t.engine
}

func (t *Table) ListBranches(ctx context.Context) ([]branches.Config, error) {
	return t.branches.All(ctx)
}

func (t *Table) LookupBranchByName(ctx context.Context, name string) (*branches.Config, error) {
	return t.branches.LookupByName(ctx, name)
}

func (t *Table) openBranch(ctx context.Context, config *branches.Config) (*Branch, error) {
	return OpenBranch(ctx, config, t.engine, t)
}

func (t *Table) OpenBranchByName(ctx context.Context, name string) (*Branch, error) {
	branchRef, err := t.LookupBranchByName(ctx, name)
	if err != nil {
		return nil, err
	}
	return t.openBranch(ctx, branchRef)
}

// ResolveRevision returns the commit id for revision. revision can be either a
// commit ID in string form or a branch name.
func (t *Table) ResolveRevision(ctx context.Context, revision string) (uuid.UUID, error) {
	id, err := dbid.ParseID(revision)
	if err != nil {
		branch, err := t.LookupBranchByName(ctx, revision)
		if err != nil {
			return uuid.Nil(), err
		}
		id = branch.Commit
	}
	return id, nil
}

func (t *Table) BatchifyBranches(ctx context.Context, sctx *super.Context, recs []super.Value, m *super.Marshaler, f expr.Evaluator) ([]super.Value, error) {
	branches, err := t.ListBranches(ctx)
	if err != nil {
		return nil, err
	}
	for _, branchRef := range branches {
		meta := BranchMeta{t.Config, branchRef}
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
	Commit uuid.UUID
}

func (t *Table) BatchifyBranchTips(ctx context.Context, sctx *super.Context, f expr.Evaluator) ([]super.Value, error) {
	branches, err := t.ListBranches(ctx)
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
func (t *Table) ObjectExists(ctx context.Context, id uuid.UUID) (bool, error) {
	return t.engine.Exists(ctx, data.URI(t.DataPath, id))
}

func (t *Table) Vacate(ctx context.Context, ts nano.Ts, dryrun bool) ([]uuid.UUID, error) {
	if !dryrun {
		if err := t.vacateBranchStore(ctx, ts, dryrun); err != nil {
			return nil, err
		}
	}
	return t.vacateCommits(ctx, ts, dryrun)
}

func (t *Table) vacateCommits(ctx context.Context, ts nano.Ts, dryrun bool) ([]uuid.UUID, error) {
	main, err := t.Main(ctx)
	if err != nil {
		return nil, err
	}
	commit, err := t.commits.FindNearestToTs(ctx, main.Branch.Commit, ts)
	if err != nil {
		return nil, err
	}
	branches, err := t.branches.All(ctx)
	if err != nil {
		return nil, err
	}
	var conflicts []string
	for _, b := range branches {
		// Find any branches whose history does not include commit and if any
		// are found fail.
		path, err := t.commits.Path(ctx, b.Commit)
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
		path, err := t.commits.Path(ctx, commit)
		if err != nil {
			return nil, err
		}
		return path[1:], nil
	}
	return t.commits.SetBase(ctx, commit)
}

func (t *Table) vacateBranchStore(ctx context.Context, ts nano.Ts, dryrun bool) error {
	var werr error
	at, err := t.branches.EntryWhere(ctx, func(config *branches.Config) bool {
		if config.Commit == uuid.Nil() {
			return false
		}
		var c *commits.Commit
		_, c, werr = t.commits.GetBytes(ctx, config.Commit)
		return werr != nil || ts >= c.Date
	})
	if err = errors.Join(err, werr); err != nil {
		return err
	}
	return t.branches.TruncateHistory(ctx, at)
}

func (t *Table) Vacuum(ctx context.Context, commit uuid.UUID, dryrun bool) ([]uuid.UUID, error) {
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(runtime.GOMAXPROCS(0))
	ch := make(chan *data.Object)
	group.Go(func() error {
		defer close(ch)
		return t.commits.Vacuumable(ctx, commit, ch)
	})
	var vacuumed []uuid.UUID
	var mu sync.Mutex
	for o := range ch {
		if dryrun {
			// For dryrun just check if the object exists and append existing
			// objects to list of results.
			group.Go(func() error {
				ok, err := t.engine.Exists(ctx, data.URI(t.DataPath, o.ID))
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
			err := t.engine.Delete(ctx, data.URI(t.DataPath, o.ID))
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

func (t *Table) Main(ctx context.Context) (BranchMeta, error) {
	branch, err := t.OpenBranchByName(ctx, "main")
	if err != nil {
		return BranchMeta{}, err
	}
	return BranchMeta{t.Config, branch.Config}, nil
}

func (t *Table) NewReader(ctx context.Context, sctx *super.Context, object *data.Object, pushdown vio.Pushdown, progress *vio.Progress, concurrency int) (vio.PullCloser, error) {
	uri := object.URI(t.DataPath)
	r, err := t.engine.Get(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", uri, err)
	}
	reader, err := bsupio.NewReader(ctx, sctx, r, pushdown, progress, concurrency)
	if err != nil {
		return nil, err
	}
	return &struct {
		vio.Puller
		io.Closer
	}{
		Puller: reader,
		// XXX We block close because vcache does lazy loading of
		// type values.  This relies on GC to close files.  We need
		// to work out a better way at some point.
		Closer: io.NopCloser(r),
	}, nil
}

func DataPath(tablePath *storage.URI) *storage.URI {
	return tablePath.JoinPath(DataTag)
}
