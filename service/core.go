package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/pprof"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/superdb/super"
	"github.com/superdb/super/api"
	"github.com/superdb/super/compiler"
	"github.com/superdb/super/db"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime"
	"go.uber.org/zap"
)

// DefaultFormat is the default data format that the server will assume if the
// value for a request's "Accept" or "Content-Type" headers are not set or set
// to "*/*".
const DefaultFormat = "sup"

const indexPage = `
<!DOCTYPE html>
<html>
  <title>SuperDB database service</title>
  <body style="padding:10px">
    <h2>super db serve</h2>
    <p>A <a href="https://superdb.org/book/command/db-serve.html">SuperDB database service</a> is listening on this host/port.</p>
    <p>If you're a <a href="https://zui.brimdata.io/">Zui</a> user, connect to this host/port from Zui app in the graphical desktop interface in your operating system (not a web browser).</p>
    <p>If your goal is to perform command line operations against this database, use the <a href="https://superdb.org/book/command/db.html"><code>super db</code></a> command.</p>
  </body>
</html>`

type Config struct {
	Auth                  AuthConfig
	CORSAllowedOrigins    []string
	DefaultResponseFormat string
	Root                  *storage.URI
	RootContent           io.ReadSeeker
	Version               string
	Logger                *zap.Logger
}

type Core struct {
	auth             *Auth0Authenticator
	compiler         runtime.Compiler
	conf             Config
	engine           storage.Engine
	logger           *zap.Logger
	registry         *prometheus.Registry
	root             *db.Root
	routerAPI        *mux.Router
	routerAux        *mux.Router
	runningQueries   map[string]*queryStatus
	runningQueriesMu sync.Mutex
	subscriptions    map[chan event]struct{}
	subscriptionsMu  sync.RWMutex
}

func NewCore(ctx context.Context, conf Config) (*Core, error) {
	if conf.DefaultResponseFormat == "" {
		conf.DefaultResponseFormat = DefaultFormat
	}
	if _, err := api.FormatToMediaType(conf.DefaultResponseFormat); err != nil {
		return nil, fmt.Errorf("invalid default response format: %w", err)
	}
	if conf.Logger == nil {
		conf.Logger = zap.NewNop()
	}
	if conf.RootContent == nil {
		conf.RootContent = strings.NewReader(indexPage)
	}
	if conf.Version == "" {
		conf.Version = "unknown"
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector())

	var authenticator *Auth0Authenticator
	if conf.Auth.Enabled {
		var err error
		if authenticator, err = NewAuthenticator(ctx, conf.Logger, registry, conf.Auth); err != nil {
			return nil, err
		}
	}
	path := conf.Root
	if path == nil {
		return nil, errors.New("no database root path")
	}
	var engine storage.Engine
	switch storage.Scheme(path.Scheme) {
	case storage.FileScheme:
		engine = storage.NewLocalEngine()
	case storage.S3Scheme:
		engine = storage.NewRemoteEngine()
	default:
		return nil, fmt.Errorf("root path cannot have scheme %q", path.Scheme)
	}
	root, err := db.CreateOrOpen(ctx, engine, conf.Logger.Named("db"), path)
	if err != nil {
		return nil, err
	}

	routerAux := mux.NewRouter()
	routerAux.Use(corsMiddleware(conf.CORSAllowedOrigins))

	routerAux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "", time.Time{}, conf.RootContent)
	})

	debug := routerAux.PathPrefix("/debug/pprof").Subrouter()
	debug.HandleFunc("/cmdline", pprof.Cmdline)
	debug.HandleFunc("/profile", pprof.Profile)
	debug.HandleFunc("/symbol", pprof.Symbol)
	debug.HandleFunc("/trace", pprof.Trace)
	debug.PathPrefix("/").HandlerFunc(pprof.Index)

	routerAux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	routerAux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	})
	routerAux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&api.VersionResponse{Version: conf.Version})
	})

	routerAPI := mux.NewRouter().UseEncodedPath()
	routerAPI.Use(requestIDMiddleware())
	routerAPI.Use(accessLogMiddleware(conf.Logger))
	routerAPI.Use(panicCatchMiddleware(conf.Logger))
	routerAPI.Use(corsMiddleware(conf.CORSAllowedOrigins))

	c := &Core{
		auth:           authenticator,
		compiler:       compiler.NewCompilerForDB(root),
		conf:           conf,
		engine:         engine,
		logger:         conf.Logger.Named("core"),
		root:           root,
		registry:       registry,
		routerAPI:      routerAPI,
		routerAux:      routerAux,
		runningQueries: make(map[string]*queryStatus),
		subscriptions:  make(map[chan event]struct{}),
	}

	c.addAPIServerRoutes()
	c.logger.Info("Started",
		zap.Bool("auth_enabled", conf.Auth.Enabled),
		zap.Stringer("root", path),
		zap.String("version", conf.Version),
	)
	return c, nil
}

func (c *Core) addAPIServerRoutes() {
	c.authhandle("/auth/identity", handleAuthIdentityGet).Methods("GET")
	// /auth/method intentionally requires no authentication
	c.routerAPI.Handle("/auth/method", c.handler(handleAuthMethodGet)).Methods("GET")
	c.authhandle("/compile", handleCompile).Methods("POST")
	c.authhandle("/events", handleEvents).Methods("GET")
	c.authhandle("/pool", handlePoolPost).Methods("POST")
	c.authhandle("/pool/{pool}", handlePoolDelete).Methods("DELETE")
	c.authhandle("/pool/{pool}", handleBranchPost).Methods("POST")
	c.authhandle("/pool/{pool}", handlePoolPut).Methods("PUT")
	c.authhandle("/pool/{pool}/branch/{branch}", handleBranchGet).Methods("GET")
	c.authhandle("/pool/{pool}/branch/{branch}", handleBranchDelete).Methods("DELETE")
	c.authhandle("/pool/{pool}/branch/{branch}", handleBranchLoad).Methods("POST")
	c.authhandle("/pool/{pool}/branch/{branch}/compact", handleCompact).Methods("POST")
	c.authhandle("/pool/{pool}/branch/{branch}/delete", handleDelete).Methods("POST")
	c.authhandle("/pool/{pool}/branch/{branch}/merge/{child}", handleBranchMerge).Methods("POST")
	c.authhandle("/pool/{pool}/branch/{branch}/revert/{commit}", handleRevertPost).Methods("POST")
	c.authhandle("/pool/{pool}/revision/{revision}/vacuum", handleVacuum).Methods("POST")
	c.authhandle("/pool/{pool}/stats", handlePoolStats).Methods("GET")
	c.authhandle("/pool/{pool}/vacate", handleVacate).Methods("POST")
	c.authhandle("/query", handleQuery).Methods("OPTIONS", "POST")
	c.authhandle("/query/describe", handleQueryDescribe).Methods("OPTIONS", "POST")
	c.authhandle("/query/status/{requestID}", handleQueryStatus).Methods("GET")
}

func (c *Core) handler(f func(*Core, *ResponseWriter, *Request)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if res, req, ok := newRequest(w, r, c); ok {
			f(c, res, req)
		}
	})
}

func (c *Core) authhandle(path string, f func(*Core, *ResponseWriter, *Request)) *mux.Route {
	if c.auth != nil {
		f = c.auth.Middleware(f)
	}
	return c.routerAPI.Handle(path, c.handler(f))
}

func (c *Core) Registry() *prometheus.Registry {
	return c.registry
}

func (c *Core) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var rm mux.RouteMatch
	if c.routerAux.Match(r, &rm) {
		rm.Handler.ServeHTTP(w, r)
		return
	}
	c.routerAPI.ServeHTTP(w, r)
}

func (c *Core) publishEvent(w *ResponseWriter, name string, data any) {
	marshaler := super.NewMarshaler(super.NewContext())
	marshaler.Decorate(super.StyleSimple)
	zv, err := marshaler.Marshal(data)
	if err != nil {
		w.Logger.Error("Error marshaling published event", zap.Error(err))
		return
	}
	go func() {
		ev := event{name: name, value: zv}
		c.subscriptionsMu.RLock()
		for sub := range c.subscriptions {
			sub <- ev
		}
		c.subscriptionsMu.RUnlock()
	}()
}

func (c *Core) newQueryStatus(r *Request) *queryStatus {
	id := r.ID()
	remove := func() {
		// Have query status wait around for a few seconds after done is signaled
		// so late arriving queryStatus requests can still get the status.
		time.Sleep(10 * time.Second)
		c.runningQueriesMu.Lock()
		delete(c.runningQueries, id)
		c.runningQueriesMu.Unlock()
	}
	q := &queryStatus{remove: remove}
	q.wg.Add(1)
	c.runningQueriesMu.Lock()
	c.runningQueries[id] = q
	c.runningQueriesMu.Unlock()
	return q
}

type queryStatus struct {
	wg     sync.WaitGroup
	remove func()
	error  string
}

func (q *queryStatus) setError(err error) {
	if err != nil {
		q.error = err.Error()
	}
}

func (q *queryStatus) Done() {
	q.wg.Done()
	go q.remove()
}
