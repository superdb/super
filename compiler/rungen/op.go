package rungen

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/db"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/runtime/expr"
	"github.com/superdb/super/runtime/op"
	samexpr "github.com/superdb/super/runtime/sam/expr"
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
		rctx:            rctx,
		mctx:            super.NewContext(),
		env:             env,
		progress:        &vio.Progress{},
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
func NewPushdown(b *Builder, e dag.Expr) vio.Pushdown {
	dataFilter := &dagFilter{nil, e}
	return b.newPushdown(nil, nil, nil, dataFilter, false)
}

func (b *Builder) newPushdown(projection field.Projection, metaFilter, searchFilter, dataFilter *dagFilter, unordered bool) *pushdown {
	return &pushdown{
		builder:      b,
		projection:   projection,
		metaFilter:   metaFilter,
		searchFilter: searchFilter,
		dataFilter:   dataFilter,
		unordered:    unordered,
	}
}

func (b *Builder) lookupTable(id uuid.UUID) (*db.Table, error) {
	if b.env == nil || b.env.DB() == nil {
		return nil, errors.New("internal error: database operation requires database operating context")
	}
	// This is fast because of the table cache in the database.
	return b.env.DB().OpenTable(b.rctx.Context, id)
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
	return EvalAtCompileTimeWithEnv(context.Background(), sctx, nil, main)
}

// EvalAtCompileTimeWithEnv is like EvalAtCompileTime but env provides access
// to data so the expression may contain subqueries that read data, as with
// the query of a type declaration.
func EvalAtCompileTimeWithEnv(ctx context.Context, sctx *super.Context, env *exec.Environment, main *dag.MainExpr) (val super.Value, err error) {
	b := NewBuilder(runtime.NewContext(ctx, sctx), env)
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
	case *dag.ListerScan, *dag.HTTPScan, *dag.TableScan, *dag.DBMetaScan, *dag.TableMetaScan, *dag.CommitMetaScan, *dag.NullScan:
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
