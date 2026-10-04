package log

import (
	"errors"
	"flag"

	"github.com/superdb/super/cli/outputflags"
	"github.com/superdb/super/cli/poolflags"
	"github.com/superdb/super/cmd/super/db"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/pkg/charm"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/vector/vio"
)

var spec = &charm.Spec{
	Name:  "log",
	Usage: "log [options]",
	Short: "display the commit log history starting at any commit",
	Long: `
See https://superdb.org/command/db.html#super-db-log
`,
	New: New,
}

func init() {
	db.Spec.Add(spec)
}

type Command struct {
	*db.Command
	outputFlags outputflags.Flags
	poolFlags   poolflags.Flags
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{Command: parent.(*db.Command)}
	c.outputFlags.Format = "db"
	c.outputFlags.SetFlags(f)
	c.poolFlags.SetFlags(f)
	return c, nil
}

func (c *Command) Run(args []string) error {
	ctx, cleanup, err := c.Init(&c.outputFlags)
	if err != nil {
		return err
	}
	defer cleanup()
	if len(args) != 0 {
		return errors.New("no arguments allowed")
	}
	db, err := c.DBFlags.Open(ctx)
	if err != nil {
		return err
	}
	head, err := c.poolFlags.HEAD()
	if err != nil {
		return err
	}
	query, err := head.FromSpec("log")
	if err != nil {
		return err
	}
	if c.outputFlags.Format == "db" {
		c.outputFlags.WriterOpts.DB.Head = head.Branch
	}
	w, err := c.outputFlags.Open(ctx, storage.NewLocalEngine())
	if err != nil {
		return err
	}
	defer w.Close()
	q, err := db.Query(ctx, srcfiles.Plain(query))
	if err != nil {
		return err
	}
	defer q.Pull(true)
	err = vio.Copy(w, q)
	if closeErr := w.Close(); err == nil {
		err = closeErr
	}
	return err
}
