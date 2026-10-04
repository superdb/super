package branch

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/superdb/super/cli/outputflags"
	"github.com/superdb/super/cli/poolflags"
	"github.com/superdb/super/cmd/super/db"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/db/api"
	"github.com/superdb/super/dbid"
	"github.com/superdb/super/pkg/charm"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/vector/vio"
)

var spec = &charm.Spec{
	Name:  "branch",
	Usage: "branch new-branch [base]",
	Short: "create a new branch",
	Long: `
See https://superdb.org/command/db.html#super-db-branch
`,
	New: New,
}

type Command struct {
	*db.Command
	delete      bool
	outputFlags outputflags.Flags
	poolFlags   poolflags.Flags
}

func init() {
	db.Spec.Add(spec)
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{Command: parent.(*db.Command)}
	f.BoolVar(&c.delete, "d", false, "delete the branch instead of creating it")
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
	if len(args) > 1 {
		return errors.New("too many arguments")
	}
	defer cleanup()
	db, err := c.DBFlags.Open(ctx)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return c.list(ctx, db)
	}
	branchName := args[0]
	head, err := c.poolFlags.HEAD()
	if err != nil {
		return err
	}
	poolName := head.Pool
	if poolName == "" {
		return errors.New("a pool name must be included: pool@branch")
	}
	poolID, err := dbid.ParseID(poolName)
	if err != nil {
		poolID, err = db.PoolID(ctx, poolName)
		if err != nil {
			return err
		}
	}
	parentCommit, err := dbid.ParseID(head.Branch)
	if err != nil {
		parentCommit, err = db.CommitObject(ctx, poolID, head.Branch)
		if err != nil {
			return err
		}
	}
	if c.delete {
		if err := db.RemoveBranch(ctx, poolID, branchName); err != nil {
			return err
		}
		if !c.DBFlags.Quiet {
			fmt.Printf("branch deleted: %s\n", branchName)
		}
		return nil
	}
	if err := db.CreateBranch(ctx, poolID, branchName, parentCommit); err != nil {
		return err
	}
	if !c.DBFlags.Quiet {
		fmt.Printf("%q: branch created\n", branchName)
	}
	return nil
}

func (c *Command) list(ctx context.Context, db api.Interface) error {
	head, err := c.poolFlags.HEAD()
	if err != nil {
		return err
	}
	poolName := head.Pool
	if poolName == "" {
		return errors.New("must be on a checked out branch to list the branches in the same pool")
	}
	query := fmt.Sprintf("from '%s':branches", poolName)
	if c.outputFlags.Format == "db" {
		c.outputFlags.WriterOpts.DB.Head = head.Branch
	}
	w, err := c.outputFlags.Open(ctx, storage.NewLocalEngine())
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
