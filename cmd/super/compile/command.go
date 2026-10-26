package compile

import (
	"flag"

	"github.com/superdb/super/cmd/super/root"
	"github.com/superdb/super/pkg/charm"
)

var spec = &charm.Spec{
	Name:  "compile",
	Usage: "compile [ options ] query [file ...]",
	Short: "compile a SuperSQL query for inspection and debugging",
	Long: `
See https://superdb.org/command/compile.html
`,
	New:         New,
	HiddenFlags: HiddenFlags,
}

func init() {
	root.Super.Add(spec)
}

type Command struct {
	*root.Command
	shared Shared
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{Command: parent.(*root.Command)}
	c.shared.SetFlags(f)
	return c, nil
}

func (c *Command) Run(args []string) error {
	ctx, cleanup, err := c.Init(&c.shared.OutputFlags)
	if err != nil {
		return err
	}
	defer cleanup()
	return c.shared.Run(ctx, args, nil, false)
}
