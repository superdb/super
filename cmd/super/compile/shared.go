package compile

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/superdb/super"
	"github.com/superdb/super/cli/dbflags"
	"github.com/superdb/super/cli/inputflags"
	"github.com/superdb/super/cli/outputflags"
	"github.com/superdb/super/cli/queryflags"
	"github.com/superdb/super/compiler"
	"github.com/superdb/super/compiler/describe"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/compiler/sfmt"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/db"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/vector/vio"
)

const HiddenFlags = "" +
	// inputflags
	"inputcap," +
	// outputflags
	"framecap," +
	"noheader," +
	"rows," +
	"split," +
	"splitsize," +
	"unbuffered"

type Shared struct {
	OutputFlags outputflags.Flags
	inputFlags  inputflags.Flags
	queryFlags  queryflags.QueryTextFlags
	dag         bool
	optimize    bool
	parallel    int
	query       bool
}

func (s *Shared) SetFlags(fs *flag.FlagSet) {
	s.OutputFlags.SetFlags(fs)
	s.inputFlags.SetFlags(fs)
	s.queryFlags.SetFlags(fs)
	fs.BoolVar(&s.dag, "dag", false, "display output as DAG (implied by -O or -P)")
	fs.BoolVar(&s.optimize, "O", false, "display optimized DAG")
	fs.IntVar(&s.parallel, "P", 0, "display parallelized DAG")
	fs.BoolVar(&s.query, "C", false, "display DAG or AST as query text")
}

func (s *Shared) Run(ctx context.Context, args []string, dbFlags *dbflags.Flags, desc bool) error {
	if len(s.queryFlags.Query) == 0 && len(args) == 0 {
		return errors.New("no query specified")
	}
	var inputs []string
	if len(args) > 0 {
		s.queryFlags.Query = append(s.queryFlags.Query, &srcfiles.PlainInput{Text: args[0]})
		inputs = args[1:]
	}
	var root *db.Root
	if dbFlags != nil {
		var err error
		root, err = dbFlags.OpenDirect(ctx)
		if err != nil {
			return err
		}
	}
	ast, err := parser.ParseFiles(s.queryFlags.Query)
	if err != nil {
		return err
	}
	if s.parallel > 0 {
		s.optimize = true
	}
	if s.optimize || desc {
		s.dag = true
	}
	if !s.dag {
		if s.query {
			fmt.Println(sfmt.AST(ast.Parsed()))
			return nil
		}
		return s.writeValue(ctx, ast.Parsed())
	}
	if len(inputs) > 0 {
		ast.PrependFileScan(inputs)
	}
	rctx := runtime.DefaultContext()
	env := exec.NewEnvironment(storage.NewLocalEngine(), root)
	env.Dynamic = s.inputFlags.Dynamic
	env.ReaderOpts = s.inputFlags.ReaderOpts
	env.Static = s.inputFlags.Static
	dag, err := compiler.Analyze(rctx, ast, env, false)
	if err != nil {
		return err
	}
	if desc {
		description, err := describe.AnalyzeDAG(ctx, dag, env)
		if err != nil {
			return err
		}
		return s.writeValue(ctx, description)
	}
	if s.optimize {
		if err := compiler.Optimize(rctx, dag, env, s.parallel); err != nil {
			return err
		}
	}
	if s.query {
		fmt.Println(sfmt.DAG(dag))
		return nil
	}
	return s.writeValue(ctx, dag)
}

func (s *Shared) writeValue(ctx context.Context, v any) error {
	sctx := super.NewContext()
	val, err := super.Marshal(sctx, v)
	if err != nil {
		return err
	}
	writer, err := s.OutputFlags.Open(ctx, storage.NewLocalEngine())
	if err != nil {
		return err
	}
	err = vio.Copy(writer, sbuf.ValToPuller(sctx, val))
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	return err
}
