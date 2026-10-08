package copy

import (
	"errors"
	"flag"

	"github.com/superdb/super"
	"github.com/superdb/super/bsup"
	"github.com/superdb/super/bsup/loader"
	"github.com/superdb/super/cli/outputflags"
	"github.com/superdb/super/cmd/super/dev/vector"
	"github.com/superdb/super/pkg/charm"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/sbuf"
	"github.com/superdb/super/vector/vio"
)

var spec = &charm.Spec{
	Name:  "copy",
	Usage: "copy [flags] path",
	Short: "read a BSUP file and copy to the output through the vector cache",
	Long: `
The copy command reads BSUP vectors from
a BSUP storage objects (local files or s3 objects) and outputs
the reconstructed BSUP row data by exercising the vector cache.

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
	if len(args) != 1 {
		return errors.New("must be run with a single path argument")
	}
	uri, err := storage.ParseURI(args[0])
	if err != nil {
		return err
	}
	local := storage.NewLocalEngine()
	r, err := local.Get(ctx, uri)
	if err != nil {
		return err
	}
	fit := bsup.NewSeekable(super.NewContext(), r)
	frame, err := fit.Next()
	if err != nil {
		return err
	}
	colFrame, ok := frame.(*bsup.ColFrame)
	if !ok {
		return errors.New("input not in BSUP column form")
	}
	loader := loader.NewFrameLoader(colFrame)
	writer, err := c.outputFlags.Open(ctx, local)
	if err != nil {
		return err
	}
	sctx := super.NewContext()
	puller := runtime.NewProjection(sctx, loader, nil)
	if err := vio.Copy(writer, sbuf.NewDematerializer(sctx, puller)); err != nil {
		writer.Close()
		return err
	}
	return writer.Close()
}
