package compile

import (
	"errors"
	"flag"

	"github.com/superdb/super/cmd/super/compile"
	"github.com/superdb/super/cmd/super/db"
	"github.com/superdb/super/pkg/charm"
)

var spec = &charm.Spec{
	Name:  "compile",
	Usage: "compile [ options ] spq|sql",
	Short: "compile a database query for inspection and debugging",
	Long: `
The "super db compile" command is just like the "super compile" command except 
it compiles the query for a database instead of a local file system.
The primary difference here is that "from" operators on a database work with data
stored in the database whereas "from" operators on file system work with local files.
In both cases, "from" can also retrieve data from HTTP APIs via URL.

See the "super compile" command help for further information.
`,
	New:         New,
	HiddenFlags: compile.HiddenFlags,
}

func init() {
	db.Spec.Add(spec)
}

type Command struct {
	parent   *db.Command
	shared   compile.Shared
	describe bool
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{parent: parent.(*db.Command)}
	c.shared.SetFlags(f)
	f.BoolVar(&c.describe, "describe", false, "emit describe endpoint results for this query")
	return c, nil
}

func (c *Command) Run(args []string) error {
	if len(args) > 1 {
		return errors.New("too many arguments")
	}
	ctx, cleanup, err := c.parent.Init(&c.shared.OutputFlags)
	if err != nil {
		return err
	}
	defer cleanup()
	return c.shared.Run(ctx, args, &c.parent.DBFlags, c.describe)
}
