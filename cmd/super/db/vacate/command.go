package vacate

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/superdb/super/cli/tableflags"
	"github.com/superdb/super/cmd/super/db"
	"github.com/superdb/super/db/api"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/pkg/charm"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/pkg/plural"
	"github.com/superdb/super/runtime"
)

var spec = &charm.Spec{
	Name:  "vacate",
	Usage: "vacate [options] [timestamp]",
	Short: "truncate a table's commit history by removing old commits",
	Long: `
See https://superdb.org/command/db.html#super-db-vacate
`,
	New: New,
}

func init() {
	db.Spec.Add(spec)
}

type Command struct {
	*db.Command
	tableFlags tableflags.Flags
	dryrun     bool
	force      bool
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{Command: parent.(*db.Command)}
	c.tableFlags.SetFlags(f)
	f.BoolVar(&c.dryrun, "dryrun", false, "view the number of commits to be deleted")
	f.BoolVar(&c.force, "f", false, "do not prompt for confirmation")
	return c, nil
}

func (c *Command) Run(args []string) error {
	ctx, cleanup, err := c.Init()
	if err != nil {
		return err
	}
	defer cleanup()
	db, err := c.DBFlags.Open(ctx)
	if err != nil {
		return err
	}
	at, err := c.tableFlags.HEAD()
	if err != nil {
		return err
	}
	var ts nano.Ts
	if len(args) > 0 {
		ts, err = nano.ParseRFC3339Nano([]byte(args[0]))
	} else {
		ts, err = c.getTsFromCommittish(ctx, db, at)
	}
	if err != nil {
		return err
	}
	verb := "would vacate"
	if !c.dryrun {
		verb = "vacated"
		if err := c.confirm(ctx, ts); err != nil {
			return err
		}
	}
	cids, err := db.Vacate(ctx, at.Table, ts, c.dryrun)
	if err != nil {
		return err
	}
	if !c.DBFlags.Quiet {
		fmt.Printf("%s %d commit%s\n", verb, len(cids), plural.Slice(cids, "s"))
	}
	return nil
}

func (c *Command) getTsFromCommittish(ctx context.Context, db runtime.Database, at *dbid.Committish) (nano.Ts, error) {
	commit, err := api.GetCommit(ctx, db, at.Table, at.Branch)
	if err != nil {
		return 0, err
	}
	return commit.Date, nil
}

func (c *Command) confirm(ctx context.Context, ts nano.Ts) error {
	if c.force {
		return nil
	}
	fmt.Printf("Are you sure you want to vacate history order than %s? There is no going back... [y|n]\n", ts)
	var input string
	if _, err := fmt.Scanln(&input); err != nil {
		return err
	}
	input = strings.ToLower(input)
	if input == "y" || input == "yes" {
		return nil
	}
	return errors.New("operation canceled")
}
