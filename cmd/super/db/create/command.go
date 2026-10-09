package create

import (
	"errors"
	"flag"
	"fmt"

	"github.com/superdb/super/bsup"
	"github.com/superdb/super/cli/poolflags"
	"github.com/superdb/super/cmd/super/db"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/charm"
)

var spec = &charm.Spec{
	Name:  "create",
	Usage: "create [-orderby key[:asc|:desc]] name",
	Short: "create a new data pool",
	Long: `
See https://superdb.org/command/db.html#super-db-create
`,
	New: New,
}

type Command struct {
	*db.Command
	sortKey   string
	frameCap  uint64
	objectCap uint64
	use       bool
}

func init() {
	db.Spec.Add(spec)
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{Command: parent.(*db.Command)}
	f.Uint64Var(&c.frameCap, "framecap", bsup.DefaultFrameCap, "target number of values BSUP frames")
	f.Uint64Var(&c.objectCap, "objectcap", data.DefaultObjectCap, "target number of values in pool data objects")
	f.BoolVar(&c.use, "use", false, "set created pool as the current pool")
	f.StringVar(&c.sortKey, "orderby", "ts:desc", "pool key with optional :asc or :desc suffix to organize data in pool (cannot be changed)")
	return c, nil
}

func (c *Command) Run(args []string) error {
	ctx, cleanup, err := c.Init()
	if err != nil {
		return err
	}
	defer cleanup()
	if len(args) != 1 {
		return errors.New("create requires one argument")
	}
	db, err := c.DBFlags.Open(ctx)
	if err != nil {
		return err
	}
	sortKey, err := order.ParseSortKeys(c.sortKey)
	if err != nil {
		return err
	}
	poolName := args[0]
	id, err := db.CreatePool(ctx, poolName, sortKey, c.objectCap, c.frameCap)
	if err != nil {
		return err
	}
	if !c.DBFlags.Quiet {
		fmt.Printf("pool created: %s %s\n", poolName, id)
	}
	if c.use {
		if err := poolflags.WriteHead(poolName, "main"); err != nil {
			return err
		}
		if !c.DBFlags.Quiet {
			fmt.Printf("Switched to branch \"main\" on pool %q\n", poolName)
		}
	}
	return nil
}
