package rungen

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/segmentio/ksuid"
	"github.com/superdb/super"
	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/db"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/op"
	samexpr "github.com/superdb/super/runtime/sam/expr"
	"github.com/superdb/super/runtime/sam/op/meta"
	"github.com/superdb/super/runtime/sam/op/top"
	"github.com/superdb/super/runtime/sam/op/uniq"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/vector"
	"github.com/superdb/super/vector/vio"
)

var ErrJoinParents = errors.New("join requires two upstream parallel query paths")

type Builder struct {
	rctx            *runtime.Context
	mctx            *super.Context
	mapper          *super.TypeDefsMapper
	env             *exec.Environment
	progress        *vio.Progress
	debugs          *op.DebugChans
	channels        map[string][]vio.Puller
	deletes         *sync.Map
	funcs           map[string]*dag.FuncDef
	compiledVamUDFs map[string]*expr.UDF
	mu              sync.Mutex
}

func NewBuilder(rctx *runtime.Context, env *exec.Environment) *Builder {
	return &Builder{
		rctx: rctx,
		mctx: super.NewContext(),
		env:  env,
		progress: &vio.Progress{
			BytesRead:      0,
			BytesMatched:   0,
			RecordsRead:    0,
			RecordsMatched: 0,
		},
		debugs:          op.NewDebugChans(),
		channels:        make(map[string][]vio.Puller),
		funcs:           make(map[string]*dag.FuncDef),
		compiledVamUDFs: make(map[string]*expr.UDF),
	}
}

// Build builds a flowgraph for main.
func (b *Builder) Build(main *dag.Main) (map[string]vio.Puller, *op.DebugChans, error) {
	if !isEntry(main.Body, true) {
		return nil, nil, errors.New("internal error: DAG entry point is not a data source")
	}
	if len(main.Types) != 0 {
		defs, ok := super.NewTypeDefsFromBytes(main.Types)
		if !ok {
			return nil, nil, fmt.Errorf("bad typedefs: %v", main.Types)
		}
		b.mapper = super.NewTypeDefsMapper(b.rctx.Sctx, defs)
	}
	if _, err := b.compileVamMain(main, nil); err != nil {
		return nil, nil, err
	}
	channels := make(map[string]vio.Puller)
	for key, pullers := range b.channels {
		channels[key] = b.combineVam(pullers)
	}
	return channels, b.debugs, nil
}

func (b *Builder) sctx() *super.Context {
	return b.rctx.Sctx
}

func (b *Builder) Meter() vio.Meter {
	return b.progress
}

func (b *Builder) Deletes() *sync.Map {
	return b.deletes
}

func (b *Builder) lookupType(id int) (super.Type, error) {
	if typ, err := super.LookupPrimitiveByID(id); err == nil {
		return typ, nil
	}
	if b.mapper == nil {
		return nil, fmt.Errorf("internal error: type ID %d not resolved due to missing types table", id)
	}
	typ := b.mapper.LookupType(uint32(id))
	if typ == nil {
		return nil, fmt.Errorf("internal error: type ID %d not found in types table", id)
	}
	return typ, nil
}

func (b *Builder) compileLeaf(o dag.Op, parent sbuf.Puller) (sbuf.Puller, error) {
	switch v := o.(type) {
	//
	// Scanners in alphatbetical order.
	//
	case *dag.CommitMetaScan:
		var pruner samexpr.Evaluator
		if v.Tap && v.KeyPruner != nil {
			var err error
			pruner, err = compileExpr(v.KeyPruner)
			if err != nil {
				return nil, err
			}
		}
		return meta.NewCommitMetaScanner(b.rctx.Context, b.sctx(), b.env.DB(), v.Pool, v.Commit, v.Meta, pruner)
	case *dag.DBMetaScan:
		return meta.NewDBMetaScanner(b.rctx.Context, b.sctx(), b.env.DB(), v.Meta)
	case *dag.ListerScan:
		if parent != nil {
			return nil, errors.New("internal error: data source cannot have a parent operator")
		}
		pool, err := b.lookupPool(v.Pool)
		if err != nil {
			return nil, err
		}
		var pruner samexpr.Evaluator
		if v.KeyPruner != nil {
			pruner, err = compileExpr(v.KeyPruner)
			if err != nil {
				return nil, err
			}
		}
		return meta.NewSortedLister(b.rctx.Context, b.mctx, pool, v.Commit, pruner)
	case *dag.NullScan:
		return sbuf.NewPuller(sbuf.NewArray([]super.Value{super.Null})), nil
	case *dag.PoolMetaScan:
		return meta.NewPoolMetaScanner(b.rctx.Context, b.sctx(), b.env.DB(), v.ID, v.Meta)
	case *dag.SlicerOp:
		return meta.NewSlicer(parent, b.mctx), nil
	//
	// Non-scanner operators in alphabetical order.
	//
	case *dag.TopOp:
		exprs, err := b.compileSortExprs(v.Exprs)
		if err != nil {
			return nil, err
		}
		return top.New(b.sctx(), parent, v.Limit, exprs, v.Reverse), nil
	case *dag.UniqOp:
		return uniq.New(b.rctx, parent, v.Cflag), nil
	default:
		return nil, fmt.Errorf("unknown DAG operator type: %v", v)
	}
}

func (b *Builder) compileAssignmentsToLvals(assignments []dag.Assignment) ([]*samexpr.Lval, []*samexpr.Lval, error) {
	var srcs, dsts []*samexpr.Lval
	for _, a := range assignments {
		src, err := b.compileLval(a.RHS)
		if err != nil {
			return nil, nil, err
		}
		dst, err := b.compileLval(a.LHS)
		if err != nil {
			return nil, nil, err
		}
		srcs = append(srcs, src)
		dsts = append(dsts, dst)
	}
	return srcs, dsts, nil
}

// For runtime/sam/expr/filter_test.go
func NewPushdown(b *Builder, e dag.Expr) sbuf.Pushdown {
	return b.newPushdown(e, nil)
}
func (b *Builder) newPushdown(e dag.Expr, projection []field.Path) sbuf.Pushdown {
	if e == nil && projection == nil {
		return nil
	}
	return &pushdown{
		dataFilter: e,
		builder:    b,
		projection: field.NewProjection(projection),
	}
}

func (b *Builder) newMetaPushdown(e dag.Expr, projection, metaProjection []field.Path, unordered bool) *pushdown {
	return &pushdown{
		metaFilter:     e,
		builder:        b,
		projection:     field.NewProjection(projection),
		metaProjection: field.NewProjection(metaProjection),
		unordred:       unordered,
	}
}

func (b *Builder) lookupPool(id ksuid.KSUID) (*db.Pool, error) {
	if b.env == nil || b.env.DB() == nil {
		return nil, errors.New("internal error: database operation requires database operating context")
	}
	// This is fast because of the pool cache in the database.
	return b.env.DB().OpenPool(b.rctx.Context, id)
}

func (b *Builder) evalAtCompileTime(in dag.Expr) (val super.Value, err error) {
	if in == nil {
		return super.Null, nil
	}
	e, err := b.compileVamExpr(in)
	if err != nil {
		return super.Null, err
	}
	// Catch panic as the runtime will panic if there is a
	// reference to a var not in scope, a field access null this, etc.
	defer func() {
		if recover() != nil {
			val = b.sctx().NewErrorf("evalAtCompileTime")
		}
	}()
	vec := e.Eval(vector.NewStringError(b.sctx(), "evalAtCompileTime", 1))
	if vec.Len() != 1 {
		panic(vector.Format(vec))
	}
	return vector.ValueAt(nil, vec, 0), nil
}

func compileExpr(in dag.Expr) (samexpr.Evaluator, error) {
	b := NewBuilder(runtime.NewContext(context.Background(), super.NewContext()), nil)
	return b.compileExpr(in)
}

func EvalAtCompileTime(sctx *super.Context, main *dag.MainExpr) (val super.Value, err error) {
	// We pass in a nil adaptor, which causes a panic for anything adaptor
	// related, which is not currently allowed in an expression sub-query.
	b := NewBuilder(runtime.NewContext(context.Background(), sctx), nil)
	for _, f := range main.Funcs {
		b.funcs[f.Tag] = f
	}
	if len(main.Types) != 0 {
		defs, ok := super.NewTypeDefsFromBytes(main.Types)
		if !ok {
			return super.Value{}, fmt.Errorf("bad typedefs: %v", main.Types)
		}
		b.mapper = super.NewTypeDefsMapper(b.rctx.Sctx, defs)
	}
	return b.evalAtCompileTime(main.Expr)
}

func isEntry(seq dag.Seq, fileScanIsEntry bool) bool {
	if len(seq) == 0 {
		return false
	}
	switch op := seq[0].(type) {
	case *dag.ListerScan, *dag.HTTPScan, *dag.PoolScan, *dag.DBMetaScan, *dag.PoolMetaScan, *dag.CommitMetaScan, *dag.NullScan:
		return true
	case *dag.FileScan:
		return fileScanIsEntry
	case *dag.ForkOp:
		return len(op.Paths) > 0 && !slices.ContainsFunc(op.Paths, func(seq dag.Seq) bool {
			return !isEntry(seq, fileScanIsEntry)
		})
	}
	return false
}
