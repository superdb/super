package ls

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/segmentio/ksuid"
	"github.com/superdb/super/cli/outputflags"
	"github.com/superdb/super/cmd/super/db"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/pkg/charm"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/vector/vio"
)

var spec = &charm.Spec{
	Name:  "ls",
	Usage: "ls [options] [pool]",
	Short: "list pools in a database or branches in a pool",
	Long: `
See https://superdb.org/command/db.html#super-db-ls
`,
	New: New,
}

func init() {
	db.Spec.Add(spec)
}

type Command struct {
	*db.Command
	at          string
	outputFlags outputflags.Flags
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{Command: parent.(*db.Command)}
	c.outputFlags.Format = "db"
	c.outputFlags.SetFlags(f)
	return c, nil
}

func (c *Command) Run(args []string) error {
	var poolName string
	switch len(args) {
	case 0:
	case 1:
		poolName = args[0]
	default:
		return errors.New("too many arguments")
	}
	ctx, cleanup, err := c.Init(&c.outputFlags)
	if err != nil {
		return err
	}
	defer cleanup()
	local := storage.NewLocalEngine()
	db, err := c.DBFlags.Open(ctx)
	if err != nil {
		return err
	}
	var query string
	if poolName == "" {
		query = "from :pools"
	} else {
		if strings.IndexByte(poolName, '\'') >= 0 {
			return errors.New("pool name may not contain quote characters")
		}
		query = fmt.Sprintf("from '%s':branches", poolName)
	}
	//XXX at should be a date/time
	var at ksuid.KSUID
	if c.at != "" {
		at, err = ksuid.Parse(c.at)
		if err != nil {
			return err
		}
		query = fmt.Sprintf("%s at %s", query, at)
	}
	w, err := c.outputFlags.Open(ctx, local)
	if err != nil {
		return err
	}
	q, err := db.Query(ctx, srcfiles.Plain(query))
	if err != nil {
		w.Close()
		return err
	}
	defer q.Pull(true)
	err = vio.Copy(w, q)
	if closeErr := w.Close(); err == nil {
		err = closeErr
	}
	return err
}
