package mcp

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/superdb/super/cli"
	"github.com/superdb/super/cli/commitflags"
	"github.com/superdb/super/cli/logflags"
	"github.com/superdb/super/cmd/super/db"
	dbpkg "github.com/superdb/super/db"
	"github.com/superdb/super/db/api"
	"github.com/superdb/super/pkg/charm"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/service/mcp"
)

var spec = &charm.Spec{
	Name:  "mcp",
	Usage: "mcp [options] <path>",
	Short: "run a Model Context Protocol (MCP) server for a database",
	Long: `
The mcp command runs a Model Context Protocol (MCP) server for the database
at path.  Like serve, it is a service for the database, so any previously
attached database is not relevant to this command.  The server uses the MCP
stdio transport: it reads JSON-RPC requests from standard input and writes
responses to standard output, so an AI agent typically runs it as a
subprocess.  It provides tools to list and describe tables, to run SuperSQL
queries with results in JSON or SUP, and to load data into tables.  With
-readonly, queries that modify the database are rejected and loading is
disabled.

See https://superdb.org/command/db.html#super-db-mcp
`,
	New: New,
}

func init() {
	db.Spec.Add(spec)
}

type Command struct {
	*db.Command
	opts        mcp.Options
	commitFlags commitflags.Flags
	logflags    logflags.Flags
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{Command: parent.(*db.Command)}
	c.commitFlags.SetFlags(f)
	c.logflags.SetFlags(f)
	f.BoolVar(&c.opts.ReadOnly, "readonly", false, "reject queries that modify the database and disable loading")
	return c, nil
}

func (c *Command) Run(args []string) error {
	ctx, cleanup, err := c.Init()
	if err != nil {
		return err
	}
	defer cleanup()
	if len(args) != 1 {
		return errors.New("mcp requires one argument: the path to the database root")
	}
	root, err := storage.ParseURI(strings.TrimRight(args[0], "/"))
	if err != nil {
		return err
	}
	if api.IsRemote(root.String()) {
		return errors.New("mcp command available for local databases only")
	}
	if c.logflags.Config.Path == "stdout" {
		return errors.New("mcp: logs cannot go to stdout, which carries the MCP protocol")
	}
	logger, err := c.logflags.Open()
	if err != nil {
		return err
	}
	defer logger.Sync()
	database, err := api.OpenDirect(ctx, logger, root.String())
	if errors.Is(err, dbpkg.ErrNotExist) {
		return fmt.Errorf("%w\n(hint: run 'super db init %s' to initialize a database at this location)", err, root)
	}
	if err != nil {
		return err
	}
	// The -user, -message, and -meta flags are the defaults for the commit
	// message of a load.
	c.opts.Commit = c.commitFlags.CommitMessage()
	return mcp.NewServer(database, cli.Version(), c.opts).Serve(ctx, os.Stdin, os.Stdout)
}
