package exec

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/db"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/sio/anyio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type Environment struct {
	engine  storage.Engine
	sources *anyio.Sources
	db      *db.Root

	Dynamic          bool
	FuseInput        bool
	IgnoreOpenErrors bool
	ReaderOpts       anyio.ReaderOpts
	Static           bool
}

func NewEnvironment(engine storage.Engine, d *db.Root) *Environment {
	var sources *anyio.Sources
	if engine != nil {
		sources = anyio.NewSources(engine)
	}
	return &Environment{
		engine:    engine,
		sources:   sources,
		db:        d,
		FuseInput: os.Getenv("SUPER_FUSEINPUT") != "",
	}
}

func (e *Environment) Engine() storage.Engine {
	return e.engine
}

// FileType returns the fused type of the input at path or nil if the input
// is dynamically typed.  See anyio.Source.Type.
func (e *Environment) FileType(ctx context.Context, sctx *super.Context, path, format string) (super.Type, error) {
	if e.Dynamic || e.sources == nil {
		return nil, nil
	}
	src, err := e.sources.Lookup(path)
	if err != nil {
		return nil, err
	}
	opts := e.ReaderOpts
	opts.Format = format
	return src.Type(ctx, sctx, opts, e.Static)
}

func (e *Environment) IsAttached() bool {
	return e.db != nil
}

func (e *Environment) DB() *db.Root {
	return e.db
}

func (e *Environment) TableID(ctx context.Context, name string) (uuid.UUID, error) {
	if id, err := dbid.ParseID(name); err == nil {
		if _, err := e.db.OpenTable(ctx, id); err == nil {
			return id, nil
		}
	}
	return e.db.TableID(ctx, name)
}

func (e *Environment) CommitObject(ctx context.Context, id uuid.UUID, name string) (uuid.UUID, error) {
	if e.db != nil {
		return e.db.CommitObject(ctx, id, name)
	}
	return uuid.Nil(), nil
}

// TableType returns the fused type of a table at commit, or nil if no database
// is attached or the type can't be determined statically.
func (e *Environment) TableType(ctx context.Context, sctx *super.Context, tableID, commit uuid.UUID) (super.Type, error) {
	if e.db == nil {
		return nil, nil
	}
	return e.db.TableType(ctx, sctx, tableID, commit)
}

func (e *Environment) SortKeys(ctx context.Context, src dag.Op) order.SortKeys {
	if e.db != nil {
		return e.db.SortKeys(ctx, src)
	}
	return nil
}

func (e *Environment) Open(ctx context.Context, sctx *super.Context, path, format string, p vio.Pushdown, progress *vio.Progress, concurrentReaders int) (vio.ConcurrentPuller, error) {
	if path == "-" {
		path = "stdio:stdin"
	}
	src, err := e.sources.Lookup(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	file, err := src.Open(ctx, sctx, e.readerOpts(p, progress, format, concurrentReaders))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return newConcurrentPuller(path, file.Puller), nil
}

func (e *Environment) readerOpts(p vio.Pushdown, progress *vio.Progress, format string, concurrentReaders int) anyio.ReaderOpts {
	o := e.ReaderOpts
	o.Pushdown = p
	o.Progress = progress
	o.ConcurrentReaders = concurrentReaders
	if format != "" {
		o.Format = format
	}
	return o
}

func (e *Environment) OpenHTTP(ctx context.Context, sctx *super.Context, url, format, method string, headers http.Header, body io.Reader, p vio.Pushdown) (vio.Puller, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header = headers
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	file, err := anyio.NewFile(ctx, sctx, resp.Body, url, e.readerOpts(p, nil, format, 1))
	if err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	return file, nil
}

func newConcurrentPuller(path string, puller vio.Puller) vio.ConcurrentPuller {
	cp, ok := puller.(vio.ConcurrentPuller)
	if !ok {
		cp = &concurrentPuller{Puller: puller}
	}
	return &errorPrefixConcurrentPuller{cp, path}
}

type concurrentPuller struct {
	vio.Puller
	mu sync.Mutex
}

func (c *concurrentPuller) ConcurrentPull(done bool, id int) (vector.Any, error) {
	c.mu.Lock()
	// Defer to ensure lock is released if c.Puller.Pull panics.
	defer c.mu.Unlock()
	return c.Pull(done)
}

type errorPrefixConcurrentPuller struct {
	vio.ConcurrentPuller
	prefix string
}

func (e *errorPrefixConcurrentPuller) ConcurrentPull(done bool, id int) (vector.Any, error) {
	vec, err := e.ConcurrentPuller.ConcurrentPull(done, id)
	if err != nil {
		err = fmt.Errorf("%s: %w", e.prefix, err)
	}
	return vec, err
}

func (e *errorPrefixConcurrentPuller) Pull(done bool) (vector.Any, error) {
	vec, err := e.ConcurrentPuller.Pull(done)
	if err != nil {
		err = fmt.Errorf("%s: %w", e.prefix, err)
	}
	return vec, err
}
