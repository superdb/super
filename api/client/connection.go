package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/segmentio/ksuid"
	"github.com/superdb/super"
	"github.com/superdb/super/api"
	"github.com/superdb/super/api/client/auth0"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/db"
	"github.com/superdb/super/db/branches"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/runtime/exec"
	"github.com/superdb/super/sio/bsupio"
)

const (
	// DefaultPort port to connect to.
	DefaultPort      = 9867
	DefaultUserAgent = "super-client-golang"
)

var (
	// ErrPoolNotFound is returned when the specified pool does not exist.
	ErrPoolNotFound = errors.New("pool not found")
	// ErrPoolExists is returned when the specified the pool already exists.
	ErrPoolExists = errors.New("pool exists")
	// ErrBranchNotFound is returned when the specified branch does not exist.
	ErrBranchNotFound = errors.New("branch not found")
	// ErrBranchExists is returned when the specified the branch already exists.
	ErrBranchExists = errors.New("branch exists")
)

type Connection struct {
	auth          *auth0.Store
	client        *http.Client
	defaultHeader http.Header
	hostURL       string
}

// NewConnection creates a new connection with the given useragent string
// and a base URL set up to talk to http://localhost:defaultport
func NewConnection() *Connection {
	u := "http://localhost:" + strconv.Itoa(DefaultPort)
	return NewConnectionTo(u)
}

// NewConnectionTo creates a new connection with the given useragent string
// and a base URL derived from the hostURL argument.
func NewConnectionTo(hostURL string) *Connection {
	defaultHeader := http.Header{
		"Accept":       []string{api.MediaTypeBSUP},
		"Content-Type": []string{api.MediaTypeBSUP},
	}
	return &Connection{
		client:        &http.Client{},
		defaultHeader: defaultHeader,
		hostURL:       strings.TrimRight(hostURL, "/"),
	}
}

// ClientHostURL allows us to print the host in log messages and internal error messages
func (c *Connection) ClientHostURL() string {
	return c.hostURL
}

func (c *Connection) SetAuthToken(token string) {
	c.defaultHeader.Set("Authorization", "Bearer "+token)
}

func (c *Connection) SetAuthStore(store *auth0.Store) error {
	tokens, err := store.Tokens(c.hostURL)
	if err != nil || tokens == nil {
		return err
	}
	c.auth = store
	c.SetAuthToken(tokens.Access)
	return nil
}

func (c *Connection) SetUserAgent(useragent string) {
	c.defaultHeader.Set("User-Agent", useragent)
}

type Response struct {
	*http.Response
	Duration time.Duration
}

// Do sends an HTTP request and returns an HTTP response, refreshing the auth
// token if necessary.
//
// As for net/http.Client.Do, if the returned error is nil, the user is expected
// to call Response.Body.Close.
func (c *Connection) Do(req *Request) (*Response, error) {
	for i := 0; ; i++ {
		httpreq, err := req.HTTPRequest()
		if err != nil {
			return nil, err
		}
		res, err := c.client.Do(httpreq)
		if err != nil {
			return nil, err
		}
		if res.StatusCode < 200 || res.StatusCode > 299 {
			// parseError calls res.Body.Close.
			err = parseError(res)
			var reserr *ErrorResponse
			if i == 0 && res.StatusCode == 401 && errors.As(err, &reserr) && reserr.Err.Error() == "invalid token" {
				access, err := c.refreshAuthToken(req.ctx)
				if err != nil {
					return nil, err
				}
				req.Header.Set("Authorization", "Bearer "+access)
				continue
			}
		}
		return &Response{
			Response: res,
			Duration: req.Duration(),
		}, err
	}
}

func (c *Connection) doAndUnmarshal(ctx context.Context, req *Request, v any, templates ...any) error {
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	zr, err := bsupio.NewValueReader(ctx, super.NewContext(), res.Body)
	if err != nil {
		return err
	}
	defer zr.Close()
	rec, err := zr.Read()
	if err != nil || rec == nil {
		return err
	}
	m := super.NewUnmarshaler()
	m.Bind(templates...)
	return m.Unmarshal(*rec, v)
}

// parseError parses an error from an http.Response with an error status code. For now the content type of errors is assumed to be JSON.
func parseError(r *http.Response) error {
	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		return err
	}
	resErr := &ErrorResponse{Response: r}
	if r.Header.Get("Content-Type") == api.MediaTypeJSON {
		var apierr api.Error
		if err := json.Unmarshal(body, &apierr); err != nil {
			return err
		}
		resErr.Err = &apierr
	} else {
		resErr.Err = errors.New(string(body))
	}
	return resErr
}

func errIsStatus(err error, code int) bool {
	var errRes *ErrorResponse
	return errors.As(err, &errRes) && errRes.StatusCode == code
}

func (c *Connection) NewRequest(ctx context.Context, method, path string, body any) *Request {
	req := newRequest(ctx, c.hostURL, c.defaultHeader.Clone())
	req.Method = method
	req.Path = path
	req.Body = body
	return req
}

// Ping checks to see if the server and measure the time it takes to
// get back the response.
func (c *Connection) Ping(ctx context.Context) (time.Duration, error) {
	req := c.NewRequest(ctx, http.MethodGet, "/status", nil)
	res, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	res.Body.Close()
	return res.Duration, nil
}

// Version retrieves the version string from the service.
func (c *Connection) Version(ctx context.Context) (string, error) {
	req := c.NewRequest(ctx, http.MethodGet, "/version", nil)
	var res api.VersionResponse
	if err := c.doAndUnmarshal(ctx, req, &res); err != nil {
		return "", err
	}
	return res.Version, nil
}

func (c *Connection) PoolStats(ctx context.Context, id ksuid.KSUID) (exec.PoolStats, error) {
	req := c.NewRequest(ctx, http.MethodGet, path.Join("/pool", id.String(), "stats"), nil)
	var stats exec.PoolStats
	err := c.doAndUnmarshal(ctx, req, &stats)
	if errIsStatus(err, http.StatusNotFound) {
		err = ErrPoolNotFound
	}
	return stats, err
}

func (c *Connection) BranchGet(ctx context.Context, poolID ksuid.KSUID, branchName string) (api.CommitResponse, error) {
	path := urlPath("pool", poolID.String(), "branch", branchName)
	req := c.NewRequest(ctx, http.MethodGet, path, nil)
	var commit api.CommitResponse
	err := c.doAndUnmarshal(ctx, req, &commit)
	if errIsStatus(err, http.StatusNotFound) {
		err = ErrBranchNotFound
	}
	return commit, err
}

func (c *Connection) CreatePool(ctx context.Context, payload api.PoolPostRequest) (db.BranchMeta, error) {
	req := c.NewRequest(ctx, http.MethodPost, "/pool", payload)
	var meta db.BranchMeta
	err := c.doAndUnmarshal(ctx, req, &meta)
	if errIsStatus(err, http.StatusConflict) {
		err = ErrPoolExists
	}
	return meta, err
}

func (c *Connection) RenamePool(ctx context.Context, id ksuid.KSUID, put api.PoolPutRequest) error {
	req := c.NewRequest(ctx, http.MethodPut, path.Join("/pool", id.String()), put)
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

func (c *Connection) RemovePool(ctx context.Context, id ksuid.KSUID) error {
	req := c.NewRequest(ctx, http.MethodDelete, path.Join("/pool", id.String()), nil)
	res, err := c.Do(req)
	if err != nil {
		if errIsStatus(err, http.StatusNotFound) {
			return ErrPoolNotFound
		}
		return err
	}
	res.Body.Close()
	return nil
}

func (c *Connection) CreateBranch(ctx context.Context, poolID ksuid.KSUID, payload api.BranchPostRequest) (branches.Config, error) {
	req := c.NewRequest(ctx, http.MethodPost, path.Join("/pool", poolID.String()), payload)
	var branch branches.Config
	err := c.doAndUnmarshal(ctx, req, &branch)
	if errIsStatus(err, http.StatusConflict) {
		err = ErrBranchExists
	}
	return branch, err
}

func (c *Connection) MergeBranch(ctx context.Context, poolID ksuid.KSUID, childBranch, parentBranch string, message api.CommitMessage) (api.CommitResponse, error) {
	path := urlPath("pool", poolID.String(), "branch", parentBranch, "merge", childBranch)
	req := c.NewRequest(ctx, http.MethodPost, path, nil)
	if err := encodeCommitMessage(req, message); err != nil {
		return api.CommitResponse{}, err
	}
	var commit api.CommitResponse
	err := c.doAndUnmarshal(ctx, req, &commit)
	return commit, err
}

func (c *Connection) Revert(ctx context.Context, poolID ksuid.KSUID, branchName string, commitID ksuid.KSUID, message api.CommitMessage) (api.CommitResponse, error) {
	path := urlPath("pool", poolID.String(), "branch", branchName, "revert", commitID.String())
	req := c.NewRequest(ctx, http.MethodPost, path, nil)
	if err := encodeCommitMessage(req, message); err != nil {
		return api.CommitResponse{}, err
	}
	var commit api.CommitResponse
	err := c.doAndUnmarshal(ctx, req, &commit)
	return commit, err
}

// Query assembles a query from src and filenames and runs it.
//
// As for Connection.Do, if the returned error is nil, the user is expected to
// call Response.Body.Close.
func (c *Connection) Query(ctx context.Context, inputs []srcfiles.Input) (*Response, error) {
	files, err := srcfiles.Concat(inputs)
	if err != nil {
		return nil, err
	}
	body := api.QueryRequest{Query: string(files.Text)}
	req := c.NewRequest(ctx, http.MethodPost, "/query?ctrl=T", body)
	res, err := c.Do(req)
	if ae := (*api.Error)(nil); errors.As(err, &ae) && len(ae.CompilationErrors) > 0 {
		ae.CompilationErrors.Bind(files)
		return nil, ae.CompilationErrors
	}
	return res, err
}

func (c *Connection) Compact(ctx context.Context, poolID ksuid.KSUID, branchName string, objects []ksuid.KSUID, message api.CommitMessage) (api.CommitResponse, error) {
	path := urlPath("pool", poolID.String(), "branch", branchName, "compact")
	req := c.NewRequest(ctx, http.MethodPost, path, api.CompactRequest{ObjectIDs: objects})
	if err := encodeCommitMessage(req, message); err != nil {
		return api.CommitResponse{}, err
	}
	var commit api.CommitResponse
	err := c.doAndUnmarshal(ctx, req, &commit)
	return commit, err
}

// Load loads data from r.  contentType is a media type for r or the empty
// string, in which case the server will attempt to detect r's format.
func (c *Connection) Load(ctx context.Context, poolID ksuid.KSUID, branchName, contentType string, r io.Reader, message api.CommitMessage) (api.CommitResponse, error) {
	path := urlPath("pool", poolID.String(), "branch", branchName)
	req := c.NewRequest(ctx, http.MethodPost, path, r)
	req.Header.Set("Content-Type", contentType)
	if err := encodeCommitMessage(req, message); err != nil {
		return api.CommitResponse{}, err
	}
	var commit api.CommitResponse
	err := c.doAndUnmarshal(ctx, req, &commit)
	return commit, err
}

func encodeCommitMessage(req *Request, message api.CommitMessage) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	req.Header.Set("SuperDB-Commit", string(encoded))
	return nil
}

func (c *Connection) Delete(ctx context.Context, poolID ksuid.KSUID, branchName string, ids []ksuid.KSUID, message api.CommitMessage) (api.CommitResponse, error) {
	return c.delete(ctx, poolID, branchName, ids, "", message)
}

func (c *Connection) DeleteWhere(ctx context.Context, poolID ksuid.KSUID, branchName, src string, message api.CommitMessage) (api.CommitResponse, error) {
	return c.delete(ctx, poolID, branchName, nil, src, message)
}

func (c *Connection) delete(ctx context.Context, poolID ksuid.KSUID, branchName string, ids []ksuid.KSUID, where string, message api.CommitMessage) (api.CommitResponse, error) {
	path := urlPath("pool", poolID.String(), "branch", branchName, "delete")
	tags := make([]string, len(ids))
	for i, id := range ids {
		tags[i] = id.String()
	}
	req := c.NewRequest(ctx, http.MethodPost, path, api.DeleteRequest{
		ObjectIDs: tags,
		Where:     where,
	})
	if err := encodeCommitMessage(req, message); err != nil {
		return api.CommitResponse{}, err
	}
	var commit api.CommitResponse
	err := c.doAndUnmarshal(ctx, req, &commit)
	return commit, err
}

func (c *Connection) Vacate(ctx context.Context, pool string, ts nano.Ts, dryrun bool) (api.VacateResponse, error) {
	path := urlPath("pool", pool, "vacate")
	vals := make(url.Values)
	vals.Add("ts", ts.Time().Format(time.RFC3339Nano))
	if dryrun {
		vals.Add("dryrun", "true")
	}
	path += "?" + vals.Encode()
	req := c.NewRequest(ctx, http.MethodPost, path, nil)
	var res api.VacateResponse
	err := c.doAndUnmarshal(ctx, req, &res)
	return res, err
}

func (c *Connection) Vacuum(ctx context.Context, pool, revision string, dryrun bool) (api.VacuumResponse, error) {
	path := urlPath("pool", pool, "revision", revision, "vacuum")
	if dryrun {
		path += "?dryrun=true"
	}
	req := c.NewRequest(ctx, http.MethodPost, path, nil)
	var res api.VacuumResponse
	err := c.doAndUnmarshal(ctx, req, &res)
	return res, err
}

func (c *Connection) AddVectors(ctx context.Context, pool, revision string, objectIDs []ksuid.KSUID, message api.CommitMessage) (api.CommitResponse, error) {
	return c.doVector(ctx, pool, revision, objectIDs, message, http.MethodPost)
}

func (c *Connection) DeleteVectors(ctx context.Context, pool, revision string, objectIDs []ksuid.KSUID, message api.CommitMessage) (api.CommitResponse, error) {
	return c.doVector(ctx, pool, revision, objectIDs, message, http.MethodDelete)
}

func (c *Connection) doVector(ctx context.Context, pool, revision string, objectIDs []ksuid.KSUID, message api.CommitMessage, method string) (api.CommitResponse, error) {
	path := urlPath("pool", pool, "revision", revision, "vector")
	req := c.NewRequest(ctx, method, path, api.VectorRequest{ObjectIDs: objectIDs})
	if err := encodeCommitMessage(req, message); err != nil {
		return api.CommitResponse{}, err
	}
	var res api.CommitResponse
	err := c.doAndUnmarshal(ctx, req, &res)
	return res, err
}

func (c *Connection) SubscribeEvents(ctx context.Context) (*EventsClient, error) {
	req := c.NewRequest(ctx, http.MethodGet, "/events", nil)
	req.Header.Set("Accept", api.MediaTypeSUP)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	return newEventsClient(resp), nil
}

func (c *Connection) AuthMethod(ctx context.Context) (api.AuthMethodResponse, error) {
	req := c.NewRequest(ctx, http.MethodGet, "/auth/method", nil)
	var method api.AuthMethodResponse
	err := c.doAndUnmarshal(ctx, req, &method)
	return method, err
}

func (c *Connection) AuthIdentity(ctx context.Context) (api.AuthIdentityResponse, error) {
	req := c.NewRequest(ctx, http.MethodGet, "/auth/identity", nil)
	var ident api.AuthIdentityResponse
	err := c.doAndUnmarshal(ctx, req, &ident)
	return ident, err
}

func (c *Connection) refreshAuthToken(ctx context.Context) (string, error) {
	method, err := c.AuthMethod(ctx)
	if err != nil {
		return "", err
	}
	if method.Auth0 == nil {
		return "", fmt.Errorf("auth not available on database: %s", c.hostURL)
	}
	tokens, err := c.auth.Tokens(c.hostURL)
	if err != nil {
		return "", err
	}
	if tokens == nil {
		return "", fmt.Errorf("auth credentials not set for database: %s", c.hostURL)
	}
	client, err := auth0.NewClient(*method.Auth0)
	if err != nil {
		return "", err
	}
	refreshed, err := client.RefreshToken(ctx, tokens.Refresh)
	if err != nil {
		return "", err
	}
	if err := c.auth.SetTokens(c.hostURL, refreshed); err != nil {
		return "", err
	}
	c.SetAuthToken(refreshed.Access)
	return refreshed.Access, nil
}

type ErrorResponse struct {
	*http.Response
	Err error
}

func (e *ErrorResponse) Unwrap() error {
	return e.Err
}

func (e *ErrorResponse) Error() string {
	return fmt.Sprintf("status code %d: %v", e.StatusCode, e.Err)
}

func urlPath(elem ...string) string {
	var b strings.Builder
	for _, e := range elem {
		b.WriteString("/" + url.PathEscape(e))
	}
	return path.Clean(b.String())
}
