package serve

import (
	"context"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/superdb/super/cli"
	"github.com/superdb/super/cli/logflags"
	"github.com/superdb/super/cmd/super/db"
	"github.com/superdb/super/cmd/super/db/internal/dbmanage"
	"github.com/superdb/super/dbconn"
	"github.com/superdb/super/dbconn/remote"
	"github.com/superdb/super/pkg/charm"
	"github.com/superdb/super/pkg/fs"
	"github.com/superdb/super/pkg/httpd"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/service"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

var spec = &charm.Spec{
	Name:  "serve",
	Usage: "serve [options] <path>",
	Short: "service requests to a database",
	Long: `
See https://superdb.org/command/db.html#super-db-serve
`,
	HiddenFlags: "brimfd,portfile",
	New:         New,
}

func init() {
	db.Spec.Add(spec)
}

type Command struct {
	*db.Command
	conf     service.Config
	logflags logflags.Flags

	// brimfd is a file descriptor passed through by Zui desktop. If set the
	// command will exit if the fd is closed.
	brimfd          int
	listenAddr      string
	manage          time.Duration
	portFile        string
	rootContentFile string
}

func New(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{Command: parent.(*db.Command)}
	c.conf.Auth.SetFlags(f)
	c.conf.Version = cli.Version()
	c.logflags.SetFlags(f)
	f.IntVar(&c.brimfd, "brimfd", -1, "pipe read fd passed by Zui to signal Zui closure")
	f.Func("cors.origin", "CORS allowed origin (may be repeated)", func(s string) error {
		c.conf.CORSAllowedOrigins = append(c.conf.CORSAllowedOrigins, s)
		return nil
	})
	f.StringVar(&c.conf.DefaultResponseFormat, "defaultfmt", service.DefaultFormat, "default response format")
	f.StringVar(&c.listenAddr, "l", ":9867", "[addr]:port to listen on")
	f.DurationVar(&c.manage, "manage", 0, "when positive, run database maintenance tasks at this interval")
	f.StringVar(&c.portFile, "portfile", "", "write listen port to file")
	f.StringVar(&c.rootContentFile, "rootcontentfile", "", "file to serve for GET /")
	return c, nil
}

func (c *Command) Run(args []string) error {
	// Don't include SIGPIPE here or else a write to a closed socket (i.e.,
	// a broken network connection) will cancel the context on Linux.
	ctx, cleanup, err := c.InitWithSignals(nil, syscall.SIGINT, syscall.SIGTERM)
	if err != nil {
		return err
	}
	defer cleanup()
	if len(args) != 1 {
		return errors.New("serve requires one argument: the path to the database root")
	}
	root, err := storage.ParseURI(strings.TrimRight(args[0], "/"))
	if err != nil {
		return err
	}
	c.conf.Root = root
	if dbconn.IsRemote(c.conf.Root.String()) {
		return errors.New("serve command available for local databases only")
	}
	if c.rootContentFile != "" {
		f, err := fs.Open(c.rootContentFile)
		if err != nil {
			return err
		}
		defer f.Close()
		c.conf.RootContent = f
	}
	logger, err := c.logflags.Open()
	if err != nil {
		return err
	}
	defer logger.Sync()
	if c.brimfd != -1 {
		if ctx, err = c.watchBrimFd(ctx, logger); err != nil {
			return err
		}
	}
	c.conf.Logger = logger
	core, err := service.NewCore(ctx, c.conf)
	if err != nil {
		return err
	}
	srv := httpd.New(c.listenAddr, core)
	srv.SetLogger(logger.Named("httpd"))
	if err := srv.Start(ctx); err != nil {
		return err
	}
	group, ctx := errgroup.WithContext(ctx)
	if c.manage > 0 {
		conn := remote.NewConn("http://" + srv.Addr())
		group.Go(func() error {
			return dbmanage.Monitor(ctx, conn, dbmanage.Config{Interval: &c.manage}, logger.Named("manage"))
		})
	}
	if c.portFile != "" {
		if err := c.writePortFile(srv.Addr()); err != nil {
			return err
		}
	}
	group.Go(srv.Wait)
	return group.Wait()
}

func (c *Command) watchBrimFd(ctx context.Context, logger *zap.Logger) (context.Context, error) {
	if runtime.GOOS == "windows" {
		return nil, errors.New("flag -brimfd not applicable to windows")
	}
	f := os.NewFile(uintptr(c.brimfd), "brimfd")
	logger.Info("Listening to Zui process pipe", zap.String("fd", f.Name()))
	ctx, cancel := context.WithCancelCause(ctx)
	go func() {
		io.Copy(io.Discard, f)
		cancel(errors.New("Zui pipe closed"))
	}()
	return ctx, nil
}

func (c *Command) writePortFile(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	return fs.ReplaceFile(c.portFile, 0644, func(w io.Writer) error {
		_, err := w.Write([]byte(port))
		return err
	})
}
