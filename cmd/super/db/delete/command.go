package del

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"uuid"

	"github.com/superdb/super/cli/commitflags"
	"github.com/superdb/super/cli/dbflags"
	"github.com/superdb/super/cli/poolflags"
	"github.com/superdb/super/cmd/super/db"
	"github.com/superdb/super/db/api"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/pkg/charm"
)

var spec = &charm.Spec{
	Name:  "delete",
	Usage: "delete id [id ...]",
	Short: "delete data objects from a pool branch",
	Long: `
See https://superdb.org/command/db.html#super-db-delete
`,
	New: New,
}

func init() {
	db.Spec.Add(spec)
}

type Command struct {
	*db.Command
	commitFlags commitflags.Flags
	poolFlags   poolflags.Flags
	where       string
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{Command: parent.(*db.Command)}
	c.commitFlags.SetFlags(f)
	c.poolFlags.SetFlags(f)
	f.StringVar(&c.where, "where", "", "delete by pool key predicate")
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
	head, err := c.poolFlags.HEAD()
	if err != nil {
		return err
	}
	poolName := head.Pool
	if poolName == "" {
		return dbflags.ErrNoHEAD
	}
	poolID, err := db.PoolID(ctx, poolName)
	if err != nil {
		return err
	}
	var commit uuid.UUID
	if c.where != "" {
		if len(args) > 0 {
			return errors.New("too many arguments")
		}
		commit, err = c.deleteWhere(ctx, db, poolID, head.Branch)
	} else {
		commit, err = c.deleteByIDs(ctx, db, poolID, head.Branch, args)
	}
	if err != nil {
		return err
	}
	if !c.DBFlags.Quiet {
		fmt.Printf("%s delete committed\n", commit)
	}
	return nil
}

func (c *Command) deleteByIDs(ctx context.Context, db api.Interface, poolID uuid.UUID, branchName string, args []string) (uuid.UUID, error) {
	ids, err := dbid.ParseIDs(args)
	if err != nil {
		return uuid.Nil(), err
	}
	if len(ids) == 0 {
		return uuid.Nil(), errors.New("no data object IDs specified")
	}
	return db.Delete(ctx, poolID, branchName, ids, c.commitFlags.CommitMessage())
}

func (c *Command) deleteWhere(ctx context.Context, db api.Interface, poolID uuid.UUID, branchName string) (uuid.UUID, error) {
	return db.DeleteWhere(ctx, poolID, branchName, c.where, c.commitFlags.CommitMessage())
}
