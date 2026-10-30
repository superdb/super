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
	"github.com/superdb/super/db/tables"
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
	TablesTag   = "tables"
	MagicFile   = "superdb.bsup"
	MagicString = "SUPERDB"
)

var (
	ErrExist    = errors.New("database already exists")
	ErrNotExist = errors.New("database does not exist")
)

// The Root of the database represents the path prefix and configuration state
// for all of the tables in the database.
type Root struct {
	engine storage.Engine
	logger *zap.Logger
	path   *storage.URI

	tableCache *arc.ARCCache[uuid.UUID, *Table]
	tables     *tables.Store
	vCache     *vcache.Cache
}

type Magic struct {
	Magic   string `super:"magic"`
	Version int    `super:"version"`
}

func newRoot(engine storage.Engine, logger *zap.Logger, path *storage.URI) *Root {
	tableCache, err := arc.NewARC[uuid.UUID, *Table](1024)
	if err != nil {
		panic(err)
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Root{
		engine:     engine,
		logger:     logger,
		path:       path,
		tableCache: tableCache,
		vCache:     vcache.NewCache(engine),
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
	tablePath := r.path.JoinPath(TablesTag)
	var err error
	r.tables, err = tables.CreateStore(ctx, r.engine, r.logger, tablePath)
	if err != nil {
		return err
	}
	return r.writeMagic(ctx)
}

func (r *Root) loadConfig(ctx context.Context) error {
	if err := r.readMagic(ctx); err != nil {
		return err
	}
	tablePath := r.path.JoinPath(TablesTag)
	var err error
	r.tables, err = tables.OpenStore(ctx, r.engine, r.logger, tablePath)
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

func (r *Root) BatchifyTables(ctx context.Context, sctx *super.Context, f expr.Evaluator) ([]super.Value, error) {
	m := super.NewMarshaler(sctx)
	m.Decorate(super.StylePackage)
	tables, err := r.ListTables(ctx)
	if err != nil {
		return nil, err
	}
	var vals []super.Value
	for k := range tables {
		rec, err := m.Marshal(&tables[k])
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
	tableRefs, err := r.ListTables(ctx)
	if err != nil {
		return nil, err
	}
	var vals []super.Value
	for k := range tableRefs {
		table, err := r.openTable(ctx, &tableRefs[k])
		if err != nil {
			// We could have race here because a table got deleted
			// while we looped so we check and continue.
			if errors.Is(err, tables.ErrNotFound) {
				continue
			}
			return nil, err
		}
		vals, err = table.BatchifyBranches(ctx, sctx, vals, m, f)
		if err != nil {
			return nil, err
		}
	}
	return vals, nil
}

type BranchMeta struct {
	Table  tables.Config   `super:"table"`
	Branch branches.Config `super:"branch"`
}

func (r *Root) ListTables(ctx context.Context) ([]tables.Config, error) {
	return r.tables.All(ctx)
}

func (r *Root) TableID(ctx context.Context, tableName string) (uuid.UUID, error) {
	if tableName == "" {
		return uuid.Nil(), errors.New("no table name given")
	}
	tableRef := r.tables.LookupByName(ctx, tableName)
	if tableRef == nil {
		return uuid.Nil(), fmt.Errorf("%s: %w", tableName, tables.ErrNotFound)
	}
	return tableRef.ID, nil
}

func (r *Root) CommitObject(ctx context.Context, tableID uuid.UUID, branchName string) (uuid.UUID, error) {
	table, err := r.OpenTable(ctx, tableID)
	if err != nil {
		return uuid.Nil(), err
	}
	branchRef, err := table.LookupBranchByName(ctx, branchName)
	if err != nil {
		return uuid.Nil(), err
	}
	return branchRef.Commit, nil
}

// TableType returns the fused type of the table's data objects at commit.
// See Table.Type.
func (r *Root) TableType(ctx context.Context, sctx *super.Context, tableID, commit uuid.UUID) (super.Type, error) {
	table, err := r.OpenTable(ctx, tableID)
	if err != nil {
		return nil, err
	}
	return table.Type(ctx, sctx, commit)
}

func (r *Root) SortKeys(ctx context.Context, src dag.Op) order.SortKeys {
	switch src := src.(type) {
	case *dag.CommitMetaScan:
		if src.Tap {
			if config, err := r.tables.LookupByID(ctx, src.Table); err == nil {
				return config.SortKeys
			}
		}
	case *dag.ListerScan:
		if config, err := r.tables.LookupByID(ctx, src.Table); err == nil {
			return config.SortKeys
		}
	case *dag.SeqScan:
		if config, err := r.tables.LookupByID(ctx, src.Table); err == nil {
			return config.SortKeys
		}
	case *dag.TableScan:
		if config, err := r.tables.LookupByID(ctx, src.ID); err == nil {
			return config.SortKeys
		}
	}
	return nil
}

func (r *Root) OpenTable(ctx context.Context, id uuid.UUID) (*Table, error) {
	config, err := r.tables.LookupByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return r.openTable(ctx, config)
}

func (r *Root) openTable(ctx context.Context, config *tables.Config) (*Table, error) {
	if p, ok := r.tableCache.Get(config.ID); ok {
		// The cached table's config may be outdated, so rather than
		// return the table directly, we return a copy whose config we
		// can safely update without locking.
		p := *p
		p.Config = *config
		return &p, nil
	}
	p, err := OpenTable(ctx, r.engine, r.logger, r.path, config)
	if err != nil {
		return nil, err
	}
	r.tableCache.Add(config.ID, p)
	return p, nil
}

func (r *Root) RenameTable(ctx context.Context, id uuid.UUID, newName string) error {
	return r.tables.Rename(ctx, id, newName)
}

func (r *Root) CreateTable(ctx context.Context, name string, sortKeys order.SortKeys, objectCap, frameCap uint64, typ super.Type, sql bool) (*Table, error) {
	if name == "HEAD" {
		return nil, fmt.Errorf("table cannot be named %q", name)
	}
	if r.tables.LookupByName(ctx, name) != nil {
		return nil, fmt.Errorf("%s: %w", name, tables.ErrExists)
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
	config := tables.NewConfig(name, sortKeys, objectCap, frameCap)
	if typ != nil {
		config.Type = super.EncodeTypeValue(typ)
		config.SQL = sql
	}
	if err := CreateTable(ctx, r.engine, r.logger, r.path, config); err != nil {
		return nil, err
	}
	table, err := r.openTable(ctx, config)
	if err != nil {
		RemoveTable(ctx, r.engine, r.path, config)
		return nil, err
	}
	if err := r.tables.Add(ctx, config); err != nil {
		RemoveTable(ctx, r.engine, r.path, config)
		return nil, err
	}
	return table, nil
}

// RemoveTable deletes a table from the configuration journal and deletes all
// data associated with the table.
func (r *Root) RemoveTable(ctx context.Context, id uuid.UUID) error {
	config, err := r.tables.LookupByID(ctx, id)
	if err != nil {
		return err
	}
	if err := r.tables.Remove(ctx, *config); err != nil {
		return err
	}
	// This table might be cached on other cluster nodes, but that's fine.
	// With no entry in the table store, it will be inaccessible and
	// eventually evicted by the cache's LRU algorithm.
	r.tableCache.Remove(config.ID)
	return RemoveTable(ctx, r.engine, r.path, config)
}

func (r *Root) CreateBranch(ctx context.Context, tableID uuid.UUID, name string, parent uuid.UUID) (*branches.Config, error) {
	config, err := r.tables.LookupByID(ctx, tableID)
	if err != nil {
		return nil, err
	}
	return CreateBranch(ctx, r.engine, r.logger, r.path, config, name, parent)
}

func (r *Root) RemoveBranch(ctx context.Context, tableID uuid.UUID, name string) error {
	table, err := r.OpenTable(ctx, tableID)
	if err != nil {
		return err
	}
	return table.removeBranch(ctx, name)
}

// MergeBranch merges the indicated branch into its parent returning the
// commit tag of the new commit into the parent branch.
func (r *Root) MergeBranch(ctx context.Context, tableID uuid.UUID, childBranch, parentBranch, author, message string) (uuid.UUID, error) {
	table, err := r.OpenTable(ctx, tableID)
	if err != nil {
		return uuid.Nil(), err
	}
	child, err := table.OpenBranchByName(ctx, childBranch)
	if err != nil {
		return uuid.Nil(), err
	}
	parent, err := table.OpenBranchByName(ctx, parentBranch)
	if err != nil {
		return uuid.Nil(), err
	}
	return child.mergeInto(ctx, parent, author, message)
}

func (r *Root) Revert(ctx context.Context, tableID uuid.UUID, branchName string, commitID uuid.UUID, author, message string) (uuid.UUID, error) {
	table, err := r.OpenTable(ctx, tableID)
	if err != nil {
		return uuid.Nil(), err
	}
	branch, err := table.OpenBranchByName(ctx, branchName)
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
