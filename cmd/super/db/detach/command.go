package detach

import (
	"errors"
	"flag"
	"fmt"

	"github.com/superdb/super/cli/tableflags"
	"github.com/superdb/super/cmd/super/db"
	"github.com/superdb/super/dbconn"
	"github.com/superdb/super/dbconn/remote"
	"github.com/superdb/super/pkg/charm"
)

var Detach = &charm.Spec{
	Name:  "detach",
	Usage: "detach",
	Short: "detach the attached database and remove any saved service credentials",
	Long: `
See https://superdb.org/command/db.html#super-db-detach
`,
	New: NewDetachCommand,
}

func init() {
	db.Spec.Add(Detach)
}

type Command struct {
	*db.Command
}

func NewDetachCommand(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	return &Command{Command: parent.(*db.Command)}, nil
}

func (c *Command) Run(args []string) error {
	_, cleanup, err := c.Init()
	if err != nil {
		return err
	}
	defer cleanup()
	if len(args) > 0 {
		return errors.New("detach takes no arguments")
	}
	loc, err := tableflags.ReadAttach()
	if err != nil {
		return err
	}
	if loc == "" {
		return errors.New("no database attached")
	}
	if dbconn.IsRemote(loc) {
		key := remote.NewConn(loc).ClientHostURL()
		if err := c.DBFlags.AuthStore().RemoveTokens(key); err != nil {
			return fmt.Errorf("failed to remove credentials: %w", err)
		}
	}
	if err := tableflags.RemoveAttach(); err != nil {
		return err
	}
	return tableflags.ClearHead()
}
