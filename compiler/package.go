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
		// For an internal reader (like a shaper on intake), we don't do
		// any parallelization right now though this could be potentially
		// beneficial depending on where the bottleneck is for a given shaper.
		// See issue #2641.
		if err := o.Parallelize(main, parallel); err != nil {
			return err
		}
	}
	return nil
}

func Build(rctx *runtime.Context, main *dag.Main, env *exec.Environment) (map[string]vio.Puller, *op.DebugChans, vio.Meter, error) {
	b := rungen.NewBuilder(rctx, env)
	outputs, debugs, err := b.Build(main)
	if err != nil {
		return nil, nil, nil, err
	}
	return outputs, debugs, b.Meter(), nil
}

func BuildWithBuilder(rctx *runtime.Context, main *dag.Main, env *exec.Environment) (map[string]vio.Puller, *op.DebugChans, *rungen.Builder, error) {
	b := rungen.NewBuilder(rctx, env)
	outputs, debugs, err := b.Build(main)
	if err != nil {
		return nil, nil, nil, err
	}
	return outputs, debugs, b, nil
}

func CompileWithAST(rctx *runtime.Context, ast *parser.AST, env *exec.Environment, optimize bool, parallel int, readers []vio.Puller) (*exec.Query, error) {
	if len(readers) > 0 {
		env = new(*env)
		env.Stdin = vio.ConcatPuller(readers...)
	}
	main, err := Analyze(rctx, ast, env, len(readers) > 0)
	if err != nil {
		return nil, err
	}
	if optimize {
		err = Optimize(rctx, main, env, parallel)
		if err != nil {
			return nil, err
		}
	}
	outputs, debugs, meter, err := Build(rctx, main, env)
	if err != nil {
		return nil, err
	}
	return exec.NewQuery(rctx, bundleOutputs(rctx, outputs, debugs), meter), nil
}

func Compile(rctx *runtime.Context, env *exec.Environment, optimize bool, parallel int, readers []vio.Puller, inputs []srcfiles.Input) (*exec.Query, error) {
	ast, err := parser.ParseFiles(inputs)
	if err != nil {
		return nil, err
	}
	return CompileWithAST(rctx, ast, env, optimize, parallel, readers)
}

func bundleOutputs(rctx *runtime.Context, outputs map[string]vio.Puller, chans *op.DebugChans) vio.Puller {
	switch len(outputs) + len(chans.Debug) {
	case 0:
		return nil
	case 1:
		var puller vio.Puller
		for k, p := range outputs {
			puller = op.NewCatcher(op.NewSingle(k, p))
		}
		return puller
	default:
		return op.NewMux(rctx, outputs, chans)
	}
}
