package read

import (
	"errors"
	"flag"

	"uuid"
	"github.com/superdb/super"
	"github.com/superdb/super/cli/outputflags"
	"github.com/superdb/super/cmd/super/dev/vector"
	"github.com/superdb/super/pkg/charm"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime/vcache"
)

var spec = &charm.Spec{
	Name:  "project",
	Usage: "project [flags] path [field ...]",
	Short: "read a BSUP file and run a projection as a test",
	Long: `
The project command reads BSUP vectors from
BSUP storage objects (local files or s3 objects) and outputs
the reconstructed BSUP row data as a projection of zero or more fields.
If no fields are specified, all the data is projected.

This command is most useful for testing the BSUP vector cache.
`,
	New: newCommand,
}

func init() {
	vector.Spec.Add(spec)
}

type Command struct {
	*vector.Command
	outputFlags outputflags.Flags
}

func newCommand(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{Command: parent.(*vector.Command)}
	c.outputFlags.SetFlags(f)
	return c, nil
}

func (c *Command) Run(args []string) error {
	ctx, cleanup, err := c.Init(&c.outputFlags)
	if err != nil {
		return err
	}
	defer cleanup()
	if len(args) < 2 {
		return errors.New("must be run with a single path argument followed by one or more fields")
	}
	uri, err := storage.ParseURI(args[0])
	if err != nil {
		return err
	}
	var paths []field.Path
	for _, dotted := range args[1:] {
		paths = append(paths, field.Dotted(dotted))
	}
	local := storage.NewLocalEngine()
	cache := vcache.NewCache(local)
	object, err := cache.Fetch(ctx, uri, uuid.Nil())
	if err != nil {
		return err
	}
	writer, err := c.outputFlags.Open(ctx, local)
	projection := field.NewProjection(paths)
	sctx := super.NewContext()
	for _, loader := range object.Loaders() {
		vec, err := loader.Load(sctx, projection)
		if err != nil {
			writer.Close()
			return err
		}
		if err := writer.Push(vec); err != nil {
			writer.Close()
			return err
		}
	}
	return writer.Close()
}
