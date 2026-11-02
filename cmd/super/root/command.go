package root

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"

	"github.com/superdb/super"
	"github.com/superdb/super/cli"
	"github.com/superdb/super/cli/inputflags"
	"github.com/superdb/super/cli/outputflags"
	"github.com/superdb/super/cli/queryflags"
	"github.com/superdb/super/cli/runtimeflags"
	"github.com/superdb/super/compiler"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/compiler/sfmt"
	"github.com/superdb/super/pkg/charm"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/supio"
	"github.com/superdb/super/vector/vio"
)

var Super = &charm.Spec{
	Name:        "super",
	Usage:       "super [options] <command> | super [ options ] [ -c query ] [ file ... ]",
	Short:       "process data with SuperSQL queries",
	HiddenFlags: "cpuprofile,memprofile,trace",
	Long: `
See https://superdb.org/command/super.html
`,
	New:          New,
	InternalLeaf: true,
}

type Command struct {
	// common flags
	cli.Flags
	// query runtime flags
	canon        bool
	stopErr      bool
	inputFlags   inputflags.Flags
	outputFlags  outputflags.Flags
	queryFlags   queryflags.Flags
	runtimeFlags runtimeflags.Flags
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{}
	c.SetFlags(f)
	return c, nil
}

func (c *Command) SetLeafFlags(f *flag.FlagSet) {
	c.outputFlags.SetFlags(f)
	c.inputFlags.SetFlags(f)
	c.queryFlags.SetFlags(f)
	c.runtimeFlags.SetFlags(f)
	f.BoolVar(&c.canon, "C", false, "display parsed AST in a textual format")
	f.BoolVar(&c.stopErr, "e", true, "stop upon input errors")
}

func (c *Command) Run(args []string) error {
	if c.canon && len(c.queryFlags.Query) == 0 {
		return errors.New("query text must be specified (-c or -I) when using -C")
	}
	ctx, cleanup, err := c.Init(&c.inputFlags, &c.outputFlags, &c.runtimeFlags)
	if err != nil {
		return err
	}
	defer cleanup()
	if len(args) == 0 && len(c.queryFlags.Query) == 0 {
		return charm.NeedHelp
	}
	ast, err := parser.ParseFiles(c.queryFlags.Query)
	if err != nil {
		return err
	}
	if c.canon {
		fmt.Println(sfmt.AST(ast.Parsed()))
		return nil
	}
	paths, constArgs, err := splitArgs(args)
	if err != nil {
		return err
	}
	if len(paths) > 0 {
		if ast.Script() != nil {
			return errors.New("file arguments cannot be used with a script of multiple statements")
		}
		ast.PrependFileScan(paths)
	}
	if constArgs != nil {
		ast.BindArgs(constArgs)
	}
	env := exec.NewEnvironment(storage.NewLocalEngine(), nil)
	env.Dynamic = c.inputFlags.Dynamic
	env.IgnoreOpenErrors = !c.stopErr
	env.ReaderOpts = c.inputFlags.ReaderOpts
	env.Static = c.inputFlags.Static
	comp := compiler.NewCompilerWithEnv(env)
	query, err := runtime.CompileQuery(ctx, super.NewContext(), comp, ast)
	if err != nil {
		return err
	}
	defer query.Pull(true)
	writer, err := c.outputFlags.Open(ctx, env.Engine())
	if err != nil {
		return err
	}
	out := map[string]vio.Pusher{
		"main":  writer,
		"debug": supio.NewWriter(sio.NopCloser(os.Stderr), supio.WriterOpts{}),
	}
	err = vio.CopyMux(out, query)
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	c.queryFlags.PrintStats(query.Progress())
	return err
}

// splitArgs splits the command-line arguments into the input paths and the
// strings of the args constant, which follow "--".  If the only argument
// after "--" is "-", the strings of args are the lines of stdin, in which case
// stdin can't also be an input.  If there's no "--", args is nil.
func splitArgs(cmdArgs []string) ([]string, []string, error) {
	k := slices.Index(cmdArgs, "--")
	if k < 0 {
		return cmdArgs, nil, nil
	}
	paths, args := cmdArgs[:k], cmdArgs[k+1:]
	if !slices.Contains(args, "-") {
		if args == nil {
			args = []string{}
		}
		return paths, args, nil
	}
	if len(args) != 1 {
		return nil, nil, errors.New("args from stdin (- after --) cannot be used with other args")
	}
	if slices.Contains(paths, "-") || slices.Contains(paths, "stdio:stdin") {
		return nil, nil, errors.New("stdin cannot be both an input and the source of args (- after --)")
	}
	args, err := queryflags.ReadArgs(os.Stdin)
	if err != nil {
		return nil, nil, err
	}
	return paths, args, nil
}
