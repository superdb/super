package dbflags

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/superdb/super/cli/tableflags"
	"github.com/superdb/super/db"
	"github.com/superdb/super/dbconn"
	"github.com/superdb/super/dbconn/local"
	"github.com/superdb/super/dbconn/remote"
	"github.com/superdb/super/dbconn/remote/auth0"
	"github.com/superdb/super/pkg/storage"
	"go.uber.org/zap"
)

var (
	ErrNoHEAD      = errors.New("HEAD not specified: indicate with -use or run the \"use\" command")
	ErrLocalDB     = errors.New("cannot open connection on local database")
	ErrNotAttached = errors.New(`no database attached (run "super db attach <location>" or use -attach)`)
)

type Flags struct {
	ConfigDir string
	Quiet     bool
	attach    string
}

func (l *Flags) SetFlags(fs *flag.FlagSet) {
	fs.BoolVar(&l.Quiet, "q", false, "quiet mode")
	dir, _ := os.UserHomeDir()
	if dir != "" {
		dir = filepath.Join(dir, ".super")
	}
	fs.StringVar(&l.ConfigDir, "configdir", dir, "configuration and credentials directory")
	fs.Func("attach", "database for this command only (default: the attached database)",
		func(s string) error {
			l.attach = s
			if s == "" {
				return errors.New("please provide path of database or URI of service")
			}
			return nil
		})
}

func (l *Flags) Connection() (*remote.Conn, error) {
	uri, err := l.URI()
	if err != nil {
		return nil, err
	}
	if !dbconn.IsRemote(uri.String()) {
		return nil, ErrLocalDB
	}
	conn := remote.NewConn(uri.String())
	if err := conn.SetAuthStore(l.AuthStore()); err != nil {
		return nil, err
	}
	return conn, nil
}

func (l *Flags) Open(ctx context.Context) (dbconn.Conn, error) {
	uri, err := l.URI()
	if err != nil {
		return nil, err
	}
	if dbconn.IsRemote(uri.String()) {
		return l.Connection()
	}
	DB, err := local.Open(ctx, zap.Must(zap.NewProduction()), uri.String())
	if errors.Is(err, db.ErrNotExist) {
		return nil, fmt.Errorf("%w\n(hint: run 'super db init %s' to initialize a database at this location)", err, uri)
	}
	return DB, err
}

func (l *Flags) OpenDirect(ctx context.Context) (*db.Root, error) {
	uri, err := l.URI()
	if err != nil {
		return nil, err
	}
	if dbconn.IsRemote(uri.String()) {
		return nil, errors.New("cannot run on a remote database")
	}
	root, err := local.OpenDirect(ctx, zap.Must(zap.NewProduction()), uri.String())
	if errors.Is(err, db.ErrNotExist) {
		return nil, fmt.Errorf("%w\n(hint: run 'super db init %s' to initialize a database at this location)", err, uri)
	}
	return root, err
}

// URI returns the location of the database given by the -attach flag or,
// if the flag isn't given, by the attach state written by "super db attach".
func (l *Flags) URI() (*storage.URI, error) {
	s := l.attach
	if s == "" {
		var err error
		if s, err = tableflags.ReadAttach(); err != nil {
			return nil, err
		}
	}
	if s == "" {
		return nil, ErrNotAttached
	}
	return storage.ParseURI(strings.TrimRight(s, "/"))
}

func (l *Flags) AuthStore() *auth0.Store {
	return auth0.NewStore(filepath.Join(l.ConfigDir, "credentials.json"))
}
