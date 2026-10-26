package attach

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/pkg/browser"
	"github.com/superdb/super/api"
	"github.com/superdb/super/api/client"
	"github.com/superdb/super/api/client/auth0"
	"github.com/superdb/super/cli/tableflags"
	"github.com/superdb/super/cmd/super/db"
	superdb "github.com/superdb/super/db"
	dbapi "github.com/superdb/super/db/api"
	"github.com/superdb/super/pkg/charm"
	"github.com/superdb/super/pkg/storage"
	"go.uber.org/zap"
)

var Attach = &charm.Spec{
	Name:  "attach",
	Usage: "attach [options] [location]",
	Short: "attach a database or show the current attachment",
	Long: `
   The location may be a directory, an s3:// URL, or an http(s) URL of a
   SuperDB service. With no argument, attach shows the current attachment.
   See https://superdb.org/command/db.html#super-db-attach
   `,
	New: NewAttachCommand,
}

func init() {
	db.Spec.Add(Attach)
}

type Command struct {
	*db.Command
	launchBrowser bool
	login         bool
	token         string
}

func NewAttachCommand(parent charm.Command, f *flag.FlagSet) (charm.Command, error) {
	c := &Command{Command: parent.(*db.Command)}
	f.BoolVar(&c.launchBrowser, "launchbrowser", true, "launch a browser for service login")
	f.BoolVar(&c.login, "login", false, "log in even if credentials are already stored")
	f.StringVar(&c.token, "token", "", "store this raw access token instead of prompting for login")
	return c, nil
}

func (c *Command) Run(args []string) error {
	ctx, cleanup, err := c.Init()
	if err != nil {
		return err
	}
	defer cleanup()
	switch len(args) {
	case 0:
		return c.status(ctx)
	case 1:
	default:
		return errors.New("attach takes at most one location")
	}
	u, err := storage.ParseURI(strings.TrimRight(args[0], "/"))
	if err != nil {
		return err
	}
	if dbapi.IsRemote(u.String()) {
		if err := c.attachService(ctx, u.String()); err != nil {
			return err
		}
	} else if _, err := dbapi.OpenLocalDB(ctx, zap.NewNop(), u.String()); err != nil {
		if errors.Is(err, superdb.ErrNotExist) {
			return fmt.Errorf("%w\n(hint: run 'super db init %s' first)", err, u)
		}
		return err
	}
	if err := tableflags.WriteAttach(u.String()); err != nil {
		return err
	}
	if err := tableflags.ClearHead(); err != nil {
		return err
	}
	if !c.DBFlags.Quiet {
		fmt.Printf("Attached to %s\n", u)
	}
	return nil
}

func (c *Command) attachService(ctx context.Context, url string) error {
	conn := client.NewConnectionTo(url)
	if _, err := conn.Ping(ctx); err != nil {
		return fmt.Errorf("cannot reach %s: %w", url, err)
	}
	credentials := c.DBFlags.AuthStore()
	if c.token != "" {
		// when running with -token, just store the token directly.
		tokens, err := credentials.Tokens(conn.ClientHostURL())
		if err != nil {
			return err
		}
		if tokens == nil {
			tokens = &auth0.Tokens{}
		}
		tokens.Access = c.token
		return credentials.SetTokens(conn.ClientHostURL(), *tokens)
	}
	tokens, err := credentials.Tokens(conn.ClientHostURL())
	if err != nil {
		return err
	}
	if tokens != nil && !c.login {
		return nil
	}
	method, err := conn.AuthMethod(ctx)
	if err != nil {
		return fmt.Errorf("failed to obtain authentication method: %w", err)
	}
	switch method.Kind {
	case api.AuthMethodNone:
		return nil
	case api.AuthMethodAuth0:

		return c.loginAuth0(ctx, *method.Auth0, credentials, conn)
	default:
		return fmt.Errorf("service at %s requires unknown authentication method %s", conn.ClientHostURL(), method.Kind)
	}
}

func (c *Command) loginAuth0(ctx context.Context, m api.AuthMethodAuth0Details, secrets *auth0.Store, conn *client.Connection) error {
	fmt.Println("method", m.ClientID)
	fmt.Println("domain", m.Domain)
	fmt.Println("audience", m.Audience)
	auth0client, err := auth0.NewClient(m)
	if err != nil {
		return err
	}
	dcr, err := auth0client.GetDeviceCode(ctx, "openid email profile offline_access")
	if err != nil {
		return err
	}
	fmt.Println("Complete authentication at", dcr.VerificationURIComplete)
	fmt.Println("Verification code:", dcr.UserCode)
	if c.launchBrowser {
		browser.OpenURL(dcr.VerificationURIComplete)
	}
	tokens, err := auth0client.PollDeviceCodeTokens(ctx, dcr)
	if err != nil {
		return err
	}
	if err := secrets.SetTokens(conn.ClientHostURL(), tokens); err != nil {
		return fmt.Errorf("failed to save credentials file: %w", err)
	}
	fmt.Printf("Login successful, stored credentials for %s\n", conn.ClientHostURL())
	return nil
}

func (c *Command) status(ctx context.Context) error {
	u, err := c.DBFlags.URI()
	if err != nil {
		return err
	}
	fmt.Printf("Attached to %s\n", u)
	if !dbapi.IsRemote(u.String()) {
		return nil
	}
	conn, err := c.DBFlags.Connection()
	if err != nil {
		return err
	}
	method, err := conn.AuthMethod(ctx)
	if err != nil {
		return err
	}
	kind := method.Kind
	if kind == "" {
		kind = "none"
	}
	fmt.Printf("Auth method: %s\n", kind)
	if method.Kind == api.AuthMethodAuth0 {
		id, err := conn.AuthIdentity(ctx)
		if err != nil {
			fmt.Println(`Not logged in (run "super db attach -login")`)
			return nil
		}
		b, err := json.MarshalIndent(id, "", "\t")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	}
	return nil
}
