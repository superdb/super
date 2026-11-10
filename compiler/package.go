package compiler

import (
	"context"

	"github.com/superdb/super/compiler/dag"
	"github.com/superdb/super/compiler/optimizer"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/compiler/rungen"
	"github.com/superdb/super/compiler/semantic"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/runtime/op"
	"github.com/superdb/super/vector/vio"
)

func Analyze(ctx context.Context, ast *parser.AST, env *exec.Environment, extInput bool) (*dag.Main, error) {
	return semantic.Analyze(ctx, ast, env, extInput)
}

func Optimize(ctx context.Context, main *dag.Main, env *exec.Environment, parallel int) error {
	// Call optimize to possible push down a filter predicate into the
	// rungen.Reader so that the BSUP scanner can do Boyer-Moore.
	o := optimizer.New(ctx, env)
	if err := o.Optimize(main); err != nil {
		return err
	}
	if parallel > 1 {
		if err := o.Parallelize(main, parallel); err != nil {
			return err
		}
	}
	return nil
}

func Build(rctx *runtime.Context, main *dag.Main, env *exec.Environment) ([]vio.Puller, *op.Outputs, vio.Meter, error) {
	b := rungen.NewBuilder(rctx, env)
	leaves, outputs, err := b.Build(main)
	if err != nil {
		return nil, nil, nil, err
	}
	return leaves, outputs, b.Meter(), nil
}

func BuildWithBuilder(rctx *runtime.Context, main *dag.Main, env *exec.Environment) ([]vio.Puller, *op.Outputs, *rungen.Builder, error) {
	b := rungen.NewBuilder(rctx, env)
	leaves, outputs, err := b.Build(main)
	if err != nil {
		return nil, nil, nil, err
	}
	return leaves, outputs, b, nil
}

func CompileWithAST(rctx *runtime.Context, ast *parser.AST, env *exec.Environment, optimize bool, parallel int) (*exec.Query, error) {
	main, err := Analyze(rctx, ast, env, false)
	if err != nil {
		return nil, err
	}
	if optimize {
		err = Optimize(rctx, main, env, parallel)
		if err != nil {
			return nil, err
		}
	}
	leaves, outputs, meter, err := Build(rctx, main, env)
	if err != nil {
		return nil, err
	}
	return exec.NewQuery(rctx, bundleOutputs(rctx, leaves, outputs), meter), nil
}

func Compile(rctx *runtime.Context, env *exec.Environment, optimize bool, parallel int, inputs []srcfiles.Input) (*exec.Query, error) {
	ast, err := parser.ParseFiles(inputs)
	if err != nil {
		return nil, err
	}
	return CompileWithAST(rctx, ast, env, optimize, parallel)
}

// bundleOutputs returns the mux for the leaves and outputs of a flowgraph or
// nil if there are no leaves.
func bundleOutputs(rctx *runtime.Context, leaves []vio.Puller, outputs *op.Outputs) vio.Puller {
	if len(leaves) == 0 {
		return nil
	}
	return op.NewMux(rctx, outputs, leaves)
}
