// Package server assembles the HTTP service: the route table, the middleware
// chain and the start/stop lifecycle.
//
// Nothing in this package knows about a specific handler. It wires
// dependencies together and owns exactly one thing that is easy to get wrong —
// a shutdown that waits for in-flight requests instead of dropping them.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"time"

	"github.com/JerryBian/splash/internal/api"
	"github.com/JerryBian/splash/internal/auth"
	"github.com/JerryBian/splash/internal/config"
	"github.com/JerryBian/splash/internal/httpx"
	"github.com/JerryBian/splash/internal/store"
	"github.com/JerryBian/splash/internal/version"
	"github.com/JerryBian/splash/internal/web"
)

// maxHeaderBytes caps the request line plus headers. One megabyte is generous
// for a JSON API and cheap insurance against a memory-exhaustion client.
const maxHeaderBytes = 1 << 20

// maxHeaderValueCount caps repeated headers (Go 1.27). Cookie-stuffing and
// header-smuggling attempts tend to show up as absurd repetition.
const maxHeaderValueCount = 100

// Server owns the listener, the handler chain and the shutdown sequence.
type Server struct {
	cfg  config.Config
	log  *slog.Logger
	http *http.Server

	addr string // resolved listen address, set by Run

	// ready is closed once the listener is bound, so a caller can learn the
	// ephemeral port it was given instead of polling Addr.
	ready chan struct{}
}

// New builds the service from configuration and its dependencies.
//
// The handler chain is assembled here rather than in main so that tests get
// exactly the same wiring the binary runs, including middleware.
func New(cfg config.Config, log *slog.Logger, db *store.DB) (*Server, error) {
	if db == nil {
		return nil, errors.New("server: a database is required")
	}
	if log == nil {
		log = slog.Default()
	}

	authn := auth.New(auth.Options{
		Username: cfg.AdminUsername,
		Password: cfg.AdminPassword,
		Secret:   cfg.AuthSecret,
		TTL:      cfg.SessionTTL,
		Secure:   cfg.CookieSecure,
	})

	mux := http.NewServeMux()

	// The API lives on its own mux so NoCache covers exactly the API subtree:
	// a stale GET here is a bug, while the frontend's hashed assets below are
	// immutable and should be cached forever.
	apiMux := http.NewServeMux()
	api.New(api.Deps{
		Categories:          db.Categories(),
		Projects:            db.Projects(),
		Checkins:            db.Checkins(),
		Skips:               db.Skips(),
		Stats:               db.Stats(),
		Export:              db,
		Auth:                authn,
		Log:                 log,
		MaxRequestBodyBytes: cfg.MaxRequestBodyBytes,
		Environment:         cfg.Env,
		RepoURL:             version.RepoURL,
		StartedAt:           time.Now(),
	}).Register(apiMux)
	mux.Handle("/api/", httpx.NoCache(apiMux))

	// The frontend owns everything that is not an API route. Registering it
	// last is not what makes it lowest priority — the mux matches by pattern
	// specificity — but it reads that way.
	mux.Handle("/", web.Handler(web.Options{}))

	if cfg.EnablePprof {
		registerPprof(mux)
		log.Warn("pprof is enabled on /debug/pprof/; profiles expose memory contents, so never expose this port publicly")
	}

	// Middleware, outermost first. The order is deliberate:
	//
	//	RequestID  – every log line and error body below can cite the id
	//	Logging    – outside Recovery, so a recovered panic is still logged as a 500
	//	Recovery   – catches panics from everything below it
	//	SecurityHeaders, CORS – outside the mux, so even error responses carry them
	//	Auth       – innermost: reads the session cookie and labels the request
	//	             admin or anonymous before any handler runs
	middlewares := []httpx.Middleware{
		httpx.RequestID,
		httpx.Logging(log),
		httpx.Recovery(log),
		httpx.SecurityHeaders,
		httpx.CORS(httpx.CORSOptions{
			AllowedOrigins: cfg.CORSAllowedOrigins,
			MaxAge:         cfg.CORSMaxAge,
		}),
		authn.Middleware,
	}
	if cfg.IsProduction() {
		middlewares = append(middlewares, httpx.StrictTransportSecurity)
	}
	handler := httpx.Chain(mux, middlewares...)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadTimeout:       cfg.ReadTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		// Go 1.27: bound repeated header values as well as total header size.
		MaxHeaderValueCount: maxHeaderValueCount,
		// net/http's own diagnostics (listener errors, malformed requests)
		// land in the same structured log as everything else.
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelError),
	}

	return &Server{cfg: cfg, log: log, http: httpServer, ready: make(chan struct{})}, nil
}

// Ready returns a channel that is closed once the listener is bound. It is how a
// test that asked for port 0 finds out which port it actually got, without
// polling.
func (s *Server) Ready() <-chan struct{} { return s.ready }

// Handler returns the fully assembled handler chain, middleware included. Tests
// drive it through httptest without opening a port.
func (s *Server) Handler() http.Handler { return s.http.Handler }

// Addr returns the address the server is listening on. It is only meaningful
// after [Server.Run] has started, and is what makes a test able to bind ":0"
// and still learn the port it was given.
func (s *Server) Addr() string { return s.addr }

// Run serves until ctx is canceled, then shuts down gracefully.
//
// It blocks. The returned error is nil for a clean shutdown; a listen failure,
// a serve failure or a shutdown that exceeded the grace period is returned and
// should be treated as a non-zero exit.
func (s *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("server: listen on %s: %w", s.cfg.HTTPAddr, err)
	}
	s.addr = listener.Addr().String()
	close(s.ready)

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.http.Serve(listener) }()

	s.log.Info("listening",
		slog.String("addr", s.addr),
		slog.String("env", s.cfg.Env.String()),
		slog.String("version", version.Get().Version),
		slog.Bool("frontendEmbedded", web.Built()),
	)

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("server: serve: %w", err)
	case <-ctx.Done():
	}

	// signal.NotifyContext cancels with a cause naming the signal (Go 1.26),
	// so the log line says which one arrived.
	s.log.Info("shutting down",
		slog.String("reason", describeCause(context.Cause(ctx))),
		slog.Duration("grace", s.cfg.ShutdownTimeout),
	)

	// WithoutCancel is the point of this line: ctx is already canceled, and a
	// timeout derived from it would expire immediately, making every shutdown
	// a hard close.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.ShutdownTimeout)
	defer cancel()

	if err := s.http.Shutdown(shutdownCtx); err != nil {
		s.log.Warn("graceful shutdown did not finish in time; closing connections",
			slog.Any("error", err),
		)
		_ = s.http.Close()
		return fmt.Errorf("server: shutdown: %w", err)
	}

	// Serve returns ErrServerClosed once Shutdown has drained the listener.
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server: serve: %w", err)
	}
	return nil
}

// Shutdown stops the server with an explicit deadline. Run's own graceful path
// is what production uses; this is here for embedding the server in a larger
// process, and for tests.
func (s *Server) Shutdown(ctx context.Context) error { return s.http.Shutdown(ctx) }

// describeCause renders a context cancellation cause for humans.
func describeCause(cause error) string {
	if cause == nil {
		return "context canceled"
	}
	var sig os.Signal
	if errors.As(cause, &sig) {
		return "signal " + sig.String()
	}
	return cause.Error()
}

// registerPprof mounts the runtime profiling endpoints on the mux.
//
// The wildcard route is what exposes named profiles — heap, goroutine, allocs,
// block, mutex, threadcreate — and, new in Go 1.27, goroutineleak, which
// reports goroutines blocked on a primitive that can never be released. That
// last one has found more leaked-goroutine bugs in production than any other
// profile, and it costs nothing when unused.
func registerPprof(mux *http.ServeMux) {
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("POST /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	mux.HandleFunc("GET /debug/pprof/{profile}", func(w http.ResponseWriter, r *http.Request) {
		pprof.Handler(r.PathValue("profile")).ServeHTTP(w, r)
	})
}
