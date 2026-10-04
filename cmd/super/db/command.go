package db

import (
	"errors"
	"flag"
	"os"

	"github.com/superdb/super/cli/dbflags"
	"github.com/superdb/super/cli/outputflags"
	"github.com/superdb/super/cli/queryflags"
	"github.com/superdb/super/cli/runtimeflags"
	"github.com/superdb/super/cmd/super/root"
	"github.com/superdb/super/pkg/charm"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/supio"
	"github.com/superdb/super/vector/vio"
)

var Spec = &charm.Spec{
	Name:  "db",
	Usage: "db <sub-command> [options] [arguments...]",
	Short: "run database commands",
	Long: `
See https://superdb.org/command/db.html
`,
	New:          New,
	InternalLeaf: true,
}

func init() {
	root.Super.Add(Spec)
}

type Command struct {
	*root.Command
	DBFlags      dbflags.Flags
	outputFlags  outputflags.Flags
	queryFlags   queryflags.Flags
	runtimeFlags runtimeflags.Flags
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{Command: parent.(*root.Command)}
	c.DBFlags.SetFlags(f)
	return c, nil
}

func (c *Command) SetLeafFlags(f *flag.FlagSet) {
	c.outputFlags.Format = "bsup"
	c.outputFlags.SetFlags(f)
	c.queryFlags.SetFlags(f)
	c.runtimeFlags.SetFlags(f)
}

func (c *Command) Run(args []string) error {
	ctx, cleanup, err := c.Init(&c.outputFlags, &c.runtimeFlags)
	if err != nil {
		return err
	}
	defer cleanup()
	if len(args) == 0 && len(c.queryFlags.Query) == 0 {
		return charm.NeedHelp
	}
	if len(args) > 0 {
		return errors.New("super db command takes no arguments")
	}
	db, err := c.DBFlags.Open(ctx)
	if err != nil {
		return err
	}
	w, err := c.outputFlags.Open(ctx, storage.NewLocalEngine())
	if err != nil {
		return err
	}
	query, err := db.Query(ctx, c.queryFlags.Query)
	if err != nil {
		w.Close()
		return err
	}
	defer query.Pull(true)
	out := map[string]vio.Pusher{
		"main":  w,
		"debug": supio.NewWriter(sio.NopCloser(os.Stderr), supio.WriterOpts{}),
	}
	err = vio.CopyMux(out, query)
	if closeErr := w.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		c.queryFlags.PrintStats(query.Progress())
	}
	return err
}
