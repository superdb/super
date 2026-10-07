package exec

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/segmentio/ksuid"
	"github.com/superdb/super"
	"github.com/superdb/super/compiler"
	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/db"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/sam/op/meta"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/sio/anyio"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

type ConcurrentPuller interface {
	vio.Puller
	ConcurrentPull(done bool, id int) (vector.Any, error)
}

type Environment struct {
	engine storage.Engine
	db     *db.Root

	Dynamic          bool
	FuseInput        bool
	IgnoreOpenErrors bool
	ReaderOpts       anyio.ReaderOpts
	Static           bool
	Stdin            vio.Puller
}

func NewEnvironment(engine storage.Engine, d *db.Root) *Environment {
	return &Environment{
		engine:    engine,
		db:        d,
		FuseInput: os.Getenv("SUPER_FUSEINPUT") != "",
	}
}

func (e *Environment) Engine() storage.Engine {
	return e.engine
}

func (e *Environment) IsAttached() bool {
	return e.db != nil
}

func (e *Environment) DB() *db.Root {
	return e.db
}

func (e *Environment) NewQuery(rctx *runtime.Context, ast *parser.AST, inputs []vio.Puller, parallelism int) (runtime.Query, error) {
	return compiler.CompileWithAST(rctx, ast, e, true, parallelism, inputs)
}

func (e *Environment) PoolID(ctx context.Context, name string) (ksuid.KSUID, error) {
	if id, err := dbid.ParseID(name); err == nil {
		if _, err := e.db.OpenPool(ctx, id); err == nil {
			return id, nil
		}
	}
	return e.db.PoolID(ctx, name)
}

func (e *Environment) CommitObject(ctx context.Context, id ksuid.KSUID, name string) (ksuid.KSUID, error) {
	if e.db != nil {
		return e.db.CommitObject(ctx, id, name)
	}
	return ksuid.Nil, nil
}

func (e *Environment) SortKeys(ctx context.Context, src dag.Op) order.SortKeys {
	if e.db != nil {
		return e.db.SortKeys(ctx, src)
	}
	return nil
}

func (e *Environment) Open(ctx context.Context, sctx *super.Context, path, format string, p sbuf.Pushdown, concurrentReaders int) (ConcurrentPuller, error) {
	if path == "-" {
		path = "stdio:stdin"
	}
	if path == "stdio:stdin" && e.Stdin != nil {
		return newConcurrentPuller(path, e.Stdin), nil
	}
	file, err := anyio.Open(ctx, sctx, e.engine, path, e.readerOpts(p, format, concurrentReaders))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return newConcurrentPuller(path, file.Puller), nil
}

func (e *Environment) readerOpts(p sbuf.Pushdown, format string, concurrentReaders int) anyio.ReaderOpts {
	o := e.ReaderOpts
	o.Pushdown = p
	o.ConcurrentReaders = concurrentReaders
	if format != "" {
		o.Format = format
	}
	return o
}

func (e *Environment) OpenHTTP(ctx context.Context, sctx *super.Context, url, format, method string, headers http.Header, body io.Reader, p sbuf.Pushdown) (vio.Puller, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header = headers
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	file, err := anyio.NewFile(ctx, sctx, resp.Body, url, e.readerOpts(p, format, 1))
	if err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	return file, nil
}

func (e *Environment) OpenPool(ctx context.Context, sctx *super.Context, id ksuid.KSUID, pushdown sbuf.Pushdown) (vio.Puller, error) {
	pool, err := e.db.OpenPool(ctx, id)
	if err != nil {
		return nil, err
	}
	branch, err := e.CommitObject(ctx, id, "main")
	if err != nil {
		return nil, err
	}
	l, err := meta.NewSortedLister(ctx, sctx, pool, branch, nil)
	if err != nil {
		return nil, err
	}
	scanner := meta.NewSequenceScanner(runtime.NewContext(ctx, sctx), l, pool, pushdown, nil, nil)
	return sbuf.NewDematerializer(sctx, scanner), nil
}

func newConcurrentPuller(path string, puller vio.Puller) ConcurrentPuller {
	cp, ok := puller.(ConcurrentPuller)
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
	ConcurrentPuller
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
