package db

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"uuid"

	arc "github.com/hashicorp/golang-lru/arc/v2"
	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/bsupbytes"
	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/db/branches"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/db/pools"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/runtime/vcache"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio/bsupio"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/vector/vio"
	"go.uber.org/zap"
)

const (
	Version     = 6
	PoolsTag    = "pools"
	MagicFile   = "superdb.bsup"
	MagicString = "SUPERDB"
)

var (
	ErrExist    = errors.New("database already exists")
	ErrNotExist = errors.New("database does not exist")
)

// The Root of the database represents the path prefix and configuration state
// for all of the data pools in the database.
type Root struct {
	engine storage.Engine
	logger *zap.Logger
	path   *storage.URI

	poolCache *arc.ARCCache[uuid.UUID, *Pool]
	pools     *pools.Store
	vCache    *vcache.Cache
}

type Magic struct {
	Magic   string `super:"magic"`
	Version int    `super:"version"`
}

func newRoot(engine storage.Engine, logger *zap.Logger, path *storage.URI) *Root {
	poolCache, err := arc.NewARC[uuid.UUID, *Pool](1024)
	if err != nil {
		panic(err)
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Root{
		engine:    engine,
		logger:    logger,
		path:      path,
		poolCache: poolCache,
		vCache:    vcache.NewCache(engine),
	}
}

func Open(ctx context.Context, engine storage.Engine, logger *zap.Logger, path *storage.URI) (*Root, error) {
	r := newRoot(engine, logger, path)
	if err := r.loadConfig(ctx); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			err = fmt.Errorf("%s: %w", path, ErrNotExist)
		}
		return nil, err
	}
	return r, nil
}

func Create(ctx context.Context, engine storage.Engine, logger *zap.Logger, path *storage.URI) (*Root, error) {
	r := newRoot(engine, logger, path)
	if err := r.loadConfig(ctx); err == nil {
		return nil, fmt.Errorf("%s: %w", path, ErrExist)
	}
	if err := r.createConfig(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

func CreateOrOpen(ctx context.Context, engine storage.Engine, logger *zap.Logger, path *storage.URI) (*Root, error) {
	r, err := Open(ctx, engine, logger, path)
	if errors.Is(err, ErrNotExist) {
		return Create(ctx, engine, logger, path)
	}
	return r, err
}

func (r *Root) createConfig(ctx context.Context) error {
	poolPath := r.path.JoinPath(PoolsTag)
	var err error
	r.pools, err = pools.CreateStore(ctx, r.engine, r.logger, poolPath)
	if err != nil {
		return err
	}
	return r.writeMagic(ctx)
}

func (r *Root) loadConfig(ctx context.Context) error {
	if err := r.readMagic(ctx); err != nil {
		return err
	}
	poolPath := r.path.JoinPath(PoolsTag)
	var err error
	r.pools, err = pools.OpenStore(ctx, r.engine, r.logger, poolPath)
	if err != nil {
		return err
	}
	return err
}

func (r *Root) writeMagic(ctx context.Context) error {
	if err := r.readMagic(ctx); err == nil {
		return errors.New("database already exists")
	}
	magic := &Magic{
		Magic:   MagicString,
		Version: Version,
	}
	writer := bsupbytes.NewWriterWithStyle(super.StylePackage)
	if err := writer.Write(magic); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	path := r.path.JoinPath(MagicFile)
	err := r.engine.PutIfNotExists(ctx, path, writer.Bytes())
	if err == storage.ErrNotSupported {
		//XXX workaround for now: see issue #2686
		reader := bytes.NewReader(writer.Bytes())
		err = storage.Put(ctx, r.engine, path, reader)
	}
	return err
}

func (r *Root) readMagic(ctx context.Context) error {
	path := r.path.JoinPath(MagicFile)
	reader, err := r.engine.Get(ctx, path)
	if err != nil {
		return err
	}
	bsupReader, err := bsupio.NewValueReader(ctx, super.NewContext(), reader)
	if err != nil {
		return err
	}
	defer reader.Close()
	val, err := bsupReader.Read()
	if err != nil {
		return err
	}
	last, err := bsupReader.Read()
	if err != nil {
		return err
	}
	if last != nil {
		return fmt.Errorf("corrupt database version file: more than one value at %s", sup.String(last))
	}
	var magic Magic
	if err := super.Unmarshal(*val, &magic); err != nil {
		return fmt.Errorf("corrupt database version file: %w", err)
	}
	if magic.Magic != MagicString {
		return fmt.Errorf("corrupt database version file: magic %q should be %q", magic.Magic, MagicString)
	}
	if magic.Version != Version {
		return fmt.Errorf("unsupported database version: found version %d while expecting %d", magic.Version, Version)
	}
	return nil
}

func (r *Root) BatchifyPools(ctx context.Context, sctx *super.Context, f expr.Evaluator) ([]super.Value, error) {
	m := super.NewMarshaler(sctx)
	m.Decorate(super.StylePackage)
	pools, err := r.ListPools(ctx)
	if err != nil {
		return nil, err
	}
	var vals []super.Value
	for k := range pools {
		rec, err := m.Marshal(&pools[k])
		if err != nil {
			return nil, err
		}
		if filter(sctx, rec, f) {
			vals = append(vals, rec)
		}
	}
	return vals, nil
}

func (r *Root) BatchifyBranches(ctx context.Context, sctx *super.Context, f expr.Evaluator) ([]super.Value, error) {
	m := super.NewMarshaler(sctx)
	m.Decorate(super.StylePackage)
	poolRefs, err := r.ListPools(ctx)
	if err != nil {
		return nil, err
	}
	var vals []super.Value
	for k := range poolRefs {
		pool, err := r.openPool(ctx, &poolRefs[k])
		if err != nil {
			// We could have race here because a pool got deleted
			// while we looped so we check and continue.
			if errors.Is(err, pools.ErrNotFound) {
				continue
			}
			return nil, err
		}
		vals, err = pool.BatchifyBranches(ctx, sctx, vals, m, f)
		if err != nil {
			return nil, err
		}
	}
	return vals, nil
}

type BranchMeta struct {
	Pool   pools.Config    `super:"pool"`
	Branch branches.Config `super:"branch"`
}

func (r *Root) ListPools(ctx context.Context) ([]pools.Config, error) {
	return r.pools.All(ctx)
}

func (r *Root) PoolID(ctx context.Context, poolName string) (uuid.UUID, error) {
	if poolName == "" {
		return uuid.Nil(), errors.New("no pool name given")
	}
	poolRef := r.pools.LookupByName(ctx, poolName)
	if poolRef == nil {
		return uuid.Nil(), fmt.Errorf("%s: %w", poolName, pools.ErrNotFound)
	}
	return poolRef.ID, nil
}

func (r *Root) CommitObject(ctx context.Context, poolID uuid.UUID, branchName string) (uuid.UUID, error) {
	pool, err := r.OpenPool(ctx, poolID)
	if err != nil {
		return uuid.Nil(), err
	}
	branchRef, err := pool.LookupBranchByName(ctx, branchName)
	if err != nil {
		return uuid.Nil(), err
	}
	return branchRef.Commit, nil
}

func (r *Root) SortKeys(ctx context.Context, src dag.Op) order.SortKeys {
	switch src := src.(type) {
	case *dag.CommitMetaScan:
		if src.Tap {
			if config, err := r.pools.LookupByID(ctx, src.Pool); err == nil {
				return config.SortKeys
			}
		}
	case *dag.ListerScan:
		if config, err := r.pools.LookupByID(ctx, src.Pool); err == nil {
			return config.SortKeys
		}
	case *dag.PoolScan:
		if config, err := r.pools.LookupByID(ctx, src.ID); err == nil {
			return config.SortKeys
		}
	case *dag.SeqScan:
		if config, err := r.pools.LookupByID(ctx, src.Pool); err == nil {
			return config.SortKeys
		}
	}
	return nil
}

func (r *Root) OpenPool(ctx context.Context, id uuid.UUID) (*Pool, error) {
	config, err := r.pools.LookupByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return r.openPool(ctx, config)
}

func (r *Root) openPool(ctx context.Context, config *pools.Config) (*Pool, error) {
	if p, ok := r.poolCache.Get(config.ID); ok {
		// The cached pool's config may be outdated, so rather than
		// return the pool directly, we return a copy whose config we
		// can safely update without locking.
		p := *p
		p.Config = *config
		return &p, nil
	}
	p, err := OpenPool(ctx, r.engine, r.logger, r.path, config)
	if err != nil {
		return nil, err
	}
	r.poolCache.Add(config.ID, p)
	return p, nil
}

func (r *Root) RenamePool(ctx context.Context, id uuid.UUID, newName string) error {
	return r.pools.Rename(ctx, id, newName)
}

func (r *Root) CreatePool(ctx context.Context, name string, sortKeys order.SortKeys, objectCap, frameCap uint64) (*Pool, error) {
	if name == "HEAD" {
		return nil, fmt.Errorf("pool cannot be named %q", name)
	}
	if r.pools.LookupByName(ctx, name) != nil {
		return nil, fmt.Errorf("%s: %w", name, pools.ErrExists)
	}
	if len(sortKeys) > 1 {
		return nil, errors.New("secondary sort keys not yet supported")
	}
	if objectCap == 0 {
		objectCap = data.DefaultObjectCap
	}
	if frameCap == 0 {
		frameCap = bsup.DefaultFrameCap
	}
	config := pools.NewConfig(name, sortKeys, objectCap, frameCap)
	if err := CreatePool(ctx, r.engine, r.logger, r.path, config); err != nil {
		return nil, err
	}
	pool, err := r.openPool(ctx, config)
	if err != nil {
		RemovePool(ctx, r.engine, r.path, config)
		return nil, err
	}
	if err := r.pools.Add(ctx, config); err != nil {
		RemovePool(ctx, r.engine, r.path, config)
		return nil, err
	}
	return pool, nil
}

// RemovePool deletes a pool from the configuration journal and deletes all
// data associated with the pool.
func (r *Root) RemovePool(ctx context.Context, id uuid.UUID) error {
	config, err := r.pools.LookupByID(ctx, id)
	if err != nil {
		return err
	}
	if err := r.pools.Remove(ctx, *config); err != nil {
		return err
	}
	// This pool might be cached on other cluster nodes, but that's fine.
	// With no entry in the pool store, it will be inaccessible and
	// eventually evicted by the cache's LRU algorithm.
	r.poolCache.Remove(config.ID)
	return RemovePool(ctx, r.engine, r.path, config)
}

func (r *Root) CreateBranch(ctx context.Context, poolID uuid.UUID, name string, parent uuid.UUID) (*branches.Config, error) {
	config, err := r.pools.LookupByID(ctx, poolID)
	if err != nil {
		return nil, err
	}
	return CreateBranch(ctx, r.engine, r.logger, r.path, config, name, parent)
}

func (r *Root) RemoveBranch(ctx context.Context, poolID uuid.UUID, name string) error {
	pool, err := r.OpenPool(ctx, poolID)
	if err != nil {
		return err
	}
	return pool.removeBranch(ctx, name)
}

// MergeBranch merges the indicated branch into its parent returning the
// commit tag of the new commit into the parent branch.
func (r *Root) MergeBranch(ctx context.Context, poolID uuid.UUID, childBranch, parentBranch, author, message string) (uuid.UUID, error) {
	pool, err := r.OpenPool(ctx, poolID)
	if err != nil {
		return uuid.Nil(), err
	}
	child, err := pool.OpenBranchByName(ctx, childBranch)
	if err != nil {
		return uuid.Nil(), err
	}
	parent, err := pool.OpenBranchByName(ctx, parentBranch)
	if err != nil {
		return uuid.Nil(), err
	}
	return child.mergeInto(ctx, parent, author, message)
}

func (r *Root) Revert(ctx context.Context, poolID uuid.UUID, branchName string, commitID uuid.UUID, author, message string) (uuid.UUID, error) {
	pool, err := r.OpenPool(ctx, poolID)
	if err != nil {
		return uuid.Nil(), err
	}
	branch, err := pool.OpenBranchByName(ctx, branchName)
	if err != nil {
		return uuid.Nil(), err
	}
	return branch.Revert(ctx, commitID, author, message)
}

func (r *Root) Open(context.Context, *super.Context, string, string, vio.Pushdown) (sbuf.Puller, error) {
	return nil, errors.New("cannot use 'file' or 'http' source in a database query")
}

func (r *Root) VectorCache() *vcache.Cache {
	return r.vCache
}
