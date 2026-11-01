package init

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/superdb/super/cmd/super/db"
	"github.com/superdb/super/dbconn"
	"github.com/superdb/super/dbconn/local"
	"github.com/superdb/super/pkg/charm"
	"github.com/superdb/super/pkg/storage"
	"go.uber.org/zap"
)

var spec = &charm.Spec{
	Name:  "init",
	Usage: "create and initialize a new, empty database",
	Short: "init <path>",
	Long: `
See https://superdb.org/command/db.html#super-db-init
`,
	New: New,
}

func init() {
	db.Spec.Add(spec)
}

type Command struct {
	*db.Command
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	return &Command{Command: parent.(*db.Command)}, nil
}

func (c *Command) Run(args []string) error {
	ctx, cleanup, err := c.Init()
	if err != nil {
		return err
	}
	defer cleanup()
	// The database root is always given explicitly so init never consults
	// the attached database or the environment.
	if len(args) != 1 || strings.TrimRight(args[0], "/") == "" {
		return errors.New("init requires one argument: the path to the database root")
	}
	u, err := storage.ParseURI(strings.TrimRight(args[0], "/"))
	if err != nil {
		return err
	}
	if dbconn.IsRemote(u.String()) {
		return fmt.Errorf("init command not valid on remote database")
	}
	if _, err := local.Create(ctx, zap.Must(zap.NewProduction()), u.String()); err != nil {
		return err
	}
	if !c.DBFlags.Quiet {
		fmt.Printf("database created: %s\n", u)
	}
	return nil
}
