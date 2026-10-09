package load

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/paulbellamy/ratecounter"
	"github.com/superdb/super"
	"github.com/superdb/super/cli/commitflags"
	"github.com/superdb/super/cli/dbflags"
	"github.com/superdb/super/cli/inputflags"
	"github.com/superdb/super/cli/poolflags"
	"github.com/superdb/super/cli/runtimeflags"
	"github.com/superdb/super/cmd/super/db"
	"github.com/superdb/super/pkg/charm"
	"github.com/superdb/super/pkg/display"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/pkg/units"
	"github.com/superdb/super/sio/anyio"
	"github.com/superdb/super/vector/vio"
	"golang.org/x/term"
)

var spec = &charm.Spec{
	Name:  "load",
	Usage: "load [options] file|S3-object|- ...",
	Short: "add and commit data to a branch",
	Long: `
See https://superdb.org/command/db.html#super-db-load
`,
	New: New,
}

func init() {
	db.Spec.Add(spec)
}

type Command struct {
	*db.Command
	commitFlags  commitflags.Flags
	inputFlags   inputflags.Flags
	poolFlags    poolflags.Flags
	runtimeFlags runtimeflags.Flags

	// status output
	ctx       context.Context
	rate      *ratecounter.RateCounter
	engine    *engineWrap
	totalRead int64
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{Command: parent.(*db.Command)}
	c.commitFlags.SetFlags(f)
	c.inputFlags.SetFlags(f)
	c.poolFlags.SetFlags(f)
	c.runtimeFlags.SetFlags(f)
	return c, nil
}

func (c *Command) Run(args []string) error {
	ctx, cleanup, err := c.Init(&c.inputFlags, &c.runtimeFlags)
	if err != nil {
		return err
	}
	defer cleanup()
	if len(args) == 0 {
		return errors.New("super db load: at least one input file must be specified (- for stdin)")
	}
	db, err := c.DBFlags.Open(ctx)
	if err != nil {
		return err
	}
	paths := args
	c.engine = &engineWrap{Engine: storage.NewLocalEngine()}
	sctx := super.NewContext()
	readers, err := c.open(ctx, sctx, paths)
	if err != nil {
		return err
	}
	defer vio.CloseReaders(readers)
	head, err := c.poolFlags.HEAD()
	if err != nil {
		return err
	}
	if head.Pool == "" {
		return dbflags.ErrNoHEAD
	}
	poolID, err := db.PoolID(ctx, head.Pool)
	if err != nil {
		return err
	}
	var d *display.Display
	if !c.DBFlags.Quiet && term.IsTerminal(int(os.Stderr.Fd())) {
		c.ctx = ctx
		c.rate = ratecounter.NewRateCounter(time.Second)
		d = display.New(c, time.Second/2, os.Stderr)
		go d.Run()
	}
	message := c.commitFlags.CommitMessage()
	var pullers []vio.Puller
	for _, r := range readers {
		pullers = append(pullers, r)
	}
	commitID, err := db.Load(ctx, sctx, poolID, head.Branch, vio.ConcatPuller(pullers...), message)
	if d != nil {
		d.Close()
	}
	if err != nil {
		return err
	}
	if !c.DBFlags.Quiet {
		fmt.Printf("%s committed\n", commitID)
	}
	return nil
}

func (c *Command) open(ctx context.Context, sctx *super.Context, paths []string) ([]vio.PullCloser, error) {
	var readers []vio.PullCloser
	for _, path := range paths {
		if path == "-" {
			path = "stdio:stdin"
		}
		file, err := anyio.Open(ctx, sctx, c.engine, path, c.inputFlags.ReaderOpts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s\n", path, err)
			continue
		}
		readers = append(readers, file)
	}
	return readers, nil
}

func (c *Command) Display(w io.Writer) bool {
	readBytes, completed := c.engine.status()
	fmt.Fprintf(w, "(%d/%d) ", completed, len(c.engine.readers))
	rate := c.incrRate(readBytes)
	if totalBytes := c.engine.bytesTotal; totalBytes == 0 {
		fmt.Fprintf(w, "%s %s/s\n", readBytes.Abbrev(), rate.Abbrev())
	} else {
		fmt.Fprintf(w, "%s/%s %s/s %.2f%%\n", readBytes.Abbrev(), totalBytes.Abbrev(), rate.Abbrev(), float64(readBytes)/float64(totalBytes)*100)
	}
	return c.ctx.Err() == nil
}

func (c *Command) incrRate(readBytes units.Bytes) units.Bytes {
	c.rate.Incr(int64(readBytes) - c.totalRead)
	c.totalRead = int64(readBytes)
	return units.Bytes(c.rate.Rate())
}

type engineWrap struct {
	storage.Engine
	bytesTotal units.Bytes
	readers    []*byteCounter
	completed  int32
}

func (e *engineWrap) Get(ctx context.Context, u *storage.URI) (storage.Reader, error) {
	r, err := e.Engine.Get(ctx, u)
	if err != nil {
		return nil, err
	}
	size, err := storage.Size(r)
	if err != nil && !errors.Is(err, storage.ErrNotSupported) {
		return nil, err
	}
	e.bytesTotal += units.Bytes(size)
	counter := &byteCounter{Reader: r, completed: &e.completed}
	e.readers = append(e.readers, counter)
	return counter, nil
}

func (e *engineWrap) status() (units.Bytes, int) {
	var read int64
	for _, r := range e.readers {
		read += r.bytesRead()
	}
	return units.Bytes(read), int(atomic.LoadInt32(&e.completed))
}

type byteCounter struct {
	storage.Reader
	n         atomic.Int64
	completed *int32
}

func (r *byteCounter) Read(b []byte) (int, error) {
	n, err := r.Reader.Read(b)
	r.n.Add(int64(n))
	if errors.Is(err, io.EOF) {
		atomic.AddInt32(r.completed, 1)
	}
	return n, err
}

func (r *byteCounter) bytesRead() int64 {
	return r.n.Load()
}
