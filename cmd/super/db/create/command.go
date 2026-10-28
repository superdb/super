package create

import (
	"errors"
	"flag"
	"fmt"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/cli/tableflags"
	"github.com/superdb/super/cmd/super/db"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/charm"
	"github.com/superdb/super/sup"
)

var spec = &charm.Spec{
	Name:  "create",
	Usage: "create [-orderby key[:asc|:desc]] [-type type] name",
	Short: "create a new data table",
	Long: `
See https://superdb.org/command/db.html#super-db-create
`,
	New: New,
}

type Command struct {
	*db.Command
	sortKey   string
	typ       string
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
	f.Uint64Var(&c.objectCap, "objectcap", data.DefaultObjectCap, "target number of values in table data objects")
	f.BoolVar(&c.use, "use", false, "configure new table as the current table")
	f.StringVar(&c.sortKey, "orderby", "", "sort key with optional :asc or :desc suffix to organize data in a table (cannot be changed)")
	f.StringVar(&c.typ, "type", "", "SUP type to which all values loaded into the table are cast (cannot be changed)")
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
	var typ super.Type
	if c.typ != "" {
		if typ, err = sup.ParseType(super.NewContext(), c.typ); err != nil {
			return err
		}
	}
	tableName := args[0]
	id, err := db.CreateTable(ctx, tableName, sortKey, c.objectCap, c.frameCap, typ, false)
	if err != nil {
		return err
	}
	if !c.DBFlags.Quiet {
		fmt.Printf("table created: %s %s\n", tableName, id)
	}
	if c.use {
		if err := tableflags.WriteHead(tableName, "main"); err != nil {
			return err
		}
		if !c.DBFlags.Quiet {
			fmt.Printf("Switched to branch \"main\" on table %q\n", tableName)
		}
	}
	return nil
}
