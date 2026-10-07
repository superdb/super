package compiler

import (
	goruntime "runtime"

	"github.com/segmentio/ksuid"
	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/compiler/optimizer"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/db"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/runtime/sam/op/meta"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

var Parallelism = goruntime.GOMAXPROCS(0) //XXX

type compiler struct {
	env *exec.Environment
}

func NewCompiler(local storage.Engine) runtime.Environment {
	return NewCompilerWithEnv(exec.NewEnvironment(local, nil))
}

func NewCompilerForDB(root *db.Root) runtime.Environment {
	// We configure a remote storage engine into the compiler so that
	// "from" operators that source http or s3 will work, but stdio and
	// file system accesses will be rejected at open time.
	return NewCompilerWithEnv(exec.NewEnvironment(storage.NewRemoteEngine(), root))
}

func NewCompilerWithEnv(env *exec.Environment) runtime.Environment {
	return &compiler{env}
}

func NewQuery(env runtime.Environment, rctx *runtime.Context, ast *parser.AST, readers []vio.Puller, parallelism int) (runtime.Query, error) {
	if parallelism == 0 {
		parallelism = Parallelism
	}
	return CompileWithAST(rctx, ast, c.env, true, parallelism, readers)
}

func (l *compiler) NewDeleteQuery(rctx *runtime.Context, ast *parser.AST, head *dbid.Committish) (runtime.DeleteQuery, error) {
	if err := ast.ConvertToDeleteWhere(head.Pool, head.Branch); err != nil {
		return nil, err
	}
	seq := ast.Parsed()
	if len(seq) != 2 {
		return nil, &InvalidDeleteWhereQuery{}
	}
	main, err := Analyze(rctx, ast, l.env, false)
	if err != nil {
		return nil, err
	}
	if _, ok := main.Body[1].(*dag.FilterOp); !ok {
		return nil, &InvalidDeleteWhereQuery{}
	}
	if err = optimizer.New(rctx, l.env).OptimizeDeleter(main, Parallelism); err != nil {
		return nil, err
	}
	outputs, debugs, b, err := BuildWithBuilder(rctx, main, l.env)
	if err != nil {
		return nil, err
	}
	return exec.NewDeleteQuery(rctx, bundleOutputs(rctx, outputs, debugs), b.Deletes()), nil
}

func (l *compiler) NewObjectScanner(rctx *runtime.Context, poolID ksuid.KSUID, objects []*data.Object) (vio.Puller, error) {
	pool, err := l.env.DB().OpenPool(rctx, poolID)
	if err != nil {
		return nil, err
	}
	lister := meta.NewSortedListerFromObjects(rctx, rctx.Sctx, pool, objects, nil)
	slicer := meta.NewSlicer(lister, rctx.Sctx)
	return sbuf.NewDematerializer(rctx.Sctx, meta.NewSequenceScanner(rctx, slicer, pool, nil, nil, nil)), nil
}

type poolscanner struct {
	vio.Puller
	rctx *runtime.Context
}

func (p *poolscanner) Pull(done bool) (vector.Any, error) {
	if done {
		p.rctx.Cancel()
	}
	return p.Puller.Pull(done)
}

type InvalidDeleteWhereQuery struct{}

func (InvalidDeleteWhereQuery) Error() string {
	return "invalid delete where query: must be a single filter operation"
}
