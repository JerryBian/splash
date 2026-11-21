// Command server runs splash, a check-in habit tracker: a Go backend with
// the React frontend compiled into the same binary and SQLite on disk. One
// static file is the whole product — nothing else to install. The user-facing
// UI is Chinese; code, docs, API and logs are English.
//
//	go run ./cmd/server            # http://localhost:8080
//	go run ./cmd/server -seed      # also insert demo data into an empty database
//	go run ./cmd/server -pprof     # also mount /debug/pprof/
//	./splash --version             # build metadata
//
// Configuration comes from SPLASH_* environment variables, with the flags below
// taking precedence. See internal/config for the full list. The administrator
// account defaults to admin / splash123 — change SPLASH_ADMIN_PASSWORD and
// SPLASH_AUTH_SECRET before exposing the service to anyone else.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/JerryBian/splash/internal/config"
	"github.com/JerryBian/splash/internal/logger"
	"github.com/JerryBian/splash/internal/server"
	"github.com/JerryBian/splash/internal/store"
	"github.com/JerryBian/splash/internal/version"
	"github.com/JerryBian/splash/internal/web"
)

// Exit codes. Naming them means the shell status is documented rather than
// folklore: 0 is success, 1 is a runtime failure, 2 is a usage error, which is
// the convention the Go toolchain itself follows.
const (
	exitOK      = 0
	exitRuntime = 1
	exitUsage   = 2
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// run is main's testable body. It takes its context, arguments and writers as
// parameters instead of reaching for globals, so cmd/server is covered by real
// unit tests rather than by launching a process.
func run(parent context.Context, args []string, stdout, stderr io.Writer) int {
	flags, code, ok := parseFlags(args, stdout, stderr)
	if !ok {
		return code
	}

	if flags.checkAddr != "" {
		return probeHealth(flags.checkAddr, stderr)
	}

	cfg, err := config.Load()
	if err != nil {
		// Configuration is already validated by Load; report every problem at
		// once, because a boot that fails on the first typo and then on the
		// second wastes a deploy cycle.
		fmt.Fprintf(stderr, "splash: invalid configuration\n\n%s\n\nSee README.md for the full list of variables.\n", indent(err.Error()))
		return exitUsage
	}

	if err := cfg.Apply(flags.overrides()); err != nil {
		fmt.Fprintf(stderr, "splash: invalid flags\n\n%s\n", indent(err.Error()))
		return exitUsage
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(stderr, "splash: invalid configuration\n\n%s\n", indent(err.Error()))
		return exitUsage
	}

	// Logs go to the writer this function was given, not straight to os.Stdout,
	// so the tests below can assert on what the process reported.
	log, err := logger.FromConfig(cfg, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "splash: %v\n", err)
		return exitRuntime
	}
	defer func() { _ = log.Close() }()

	if !web.Built() {
		log.Warn("no frontend build is embedded in this binary; serving the placeholder page",
			slog.String("hint", "run `node scripts/build.mjs`, or `pnpm --dir web dev` for hot reload"),
		)
	}

	// NotifyContext makes SIGINT and SIGTERM cancel ctx, and — since Go 1.26 —
	// cancels it with a cause naming the signal, which the server logs.
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("starting", slog.Any("config", cfg))

	if cfg.UsingDefaultCredentials() {
		log.Warn("default admin password or auth secret is in use; set SPLASH_ADMIN_PASSWORD and SPLASH_AUTH_SECRET before exposing this service",
			slog.String("adminUsername", cfg.AdminUsername))
	}

	db, err := store.Open(cfg.DBPath, nil)
	if err != nil {
		log.Error("failed to open the database", slog.String("path", cfg.DBPath), slog.Any("error", err))
		return exitRuntime
	}
	defer func() { _ = db.Close() }()

	if cfg.SeedDemo {
		n, err := db.SeedDemo(ctx)
		if err != nil {
			log.Error("demo seed failed", slog.Any("error", err))
			return exitRuntime
		}
		if n > 0 {
			log.Info("seeded demo data", slog.Int("projects", n))
		}
	}

	srv, err := server.New(cfg, log.Logger, db)
	if err != nil {
		log.Error("failed to initialise the server", slog.Any("error", err))
		return exitRuntime
	}

	if err := srv.Run(ctx); err != nil {
		log.Error("server stopped with an error", slog.Any("error", err))
		return exitRuntime
	}
	log.Info("stopped cleanly")
	return exitOK
}

// flagSet holds the parsed command line.
type flagSet struct {
	showVersion bool
	checkAddr   string
	httpAddr    string
	logFormat   string
	logLevel    string
	logFile     string
	dbPath      string
	pprof       bool
	pprofSet    bool
	seed        bool
	seedSet     bool
}

// overrides converts the flags into the form config.Apply expects. A flag the
// user did not pass becomes a nil pointer, so it leaves the environment value
// alone.
func (f flagSet) overrides() config.Overrides {
	o := config.Overrides{
		HTTPAddr:  f.httpAddr,
		LogFormat: f.logFormat,
		LogLevel:  f.logLevel,
		LogFile:   f.logFile,
		DBPath:    f.dbPath,
	}
	if f.pprofSet {
		o.EnablePprof = new(f.pprof) // Go 1.26: new takes an expression
	}
	if f.seedSet {
		o.SeedDemo = new(f.seed)
	}
	return o
}

// parseFlags reads args. ok is false when the process should stop, with code
// holding the exit status to use.
func parseFlags(args []string, stdout, stderr io.Writer) (flagSet, int, bool) {
	var f flagSet

	fs := flag.NewFlagSet("splash", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&f.showVersion, "version", false, "print version information and exit")
	fs.StringVar(&f.checkAddr, "check", "", "probe http://ADDR/api/v1/health/ready and exit 0 or 1")
	fs.StringVar(&f.httpAddr, "http-addr", "", "listen address, e.g. :8080 (overrides SPLASH_HTTP_ADDR)")
	fs.StringVar(&f.logFormat, "log-format", "", "text or json (overrides SPLASH_LOG_FORMAT)")
	fs.StringVar(&f.logLevel, "log-level", "", "debug, info, warn or error (overrides SPLASH_LOG_LEVEL)")
	fs.StringVar(&f.logFile, "log-file", "", "also write JSON logs to this file (overrides SPLASH_LOG_FILE)")
	fs.StringVar(&f.dbPath, "db-path", "", "SQLite database file (overrides SPLASH_DB_PATH)")
	fs.BoolVar(&f.pprof, "pprof", false, "mount /debug/pprof/ (overrides SPLASH_ENABLE_PPROF)")
	fs.BoolVar(&f.seed, "seed", false, "seed demo data into an empty database (overrides SPLASH_SEED_DEMO)")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "splash %s\n\nUsage: splash [flags]\n\nFlags:\n", version.Get().Version)
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nEnvironment variables are documented in README.md and internal/config.\n")
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// -h and -help are requests, not mistakes, so they exit 0.
			return f, exitOK, false
		}
		return f, exitUsage, false
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "splash: unexpected argument %q\n\n", fs.Arg(0))
		fs.Usage()
		return f, exitUsage, false
	}

	// Visited tells us which flags the user actually passed, which is the only
	// reliable way to distinguish "-pprof=false" from "not mentioned".
	fs.Visit(func(fl *flag.Flag) {
		switch fl.Name {
		case "pprof":
			f.pprofSet = true
		case "seed":
			f.seedSet = true
		}
	})

	if f.showVersion {
		info := version.Get()
		fmt.Fprintf(stdout, "splash %s\n", info)
		fmt.Fprintf(stdout, "commit:     %s\n", info.Commit)
		fmt.Fprintf(stdout, "built:      %s\n", orUnknown(info.Date))
		fmt.Fprintf(stdout, "toolchain:  %s\n", info.GoVersion)
		fmt.Fprintf(stdout, "platform:   %s\n", info.Platform)
		fmt.Fprintf(stdout, "repository: %s\n", version.RepoURL)
		return f, exitOK, false
	}
	return f, exitOK, true
}

// probeHealth asks a running instance whether it is ready and translates the
// answer into an exit code.
//
// The runtime image is distroless: no shell, no curl, nothing else to reach an
// HTTP endpoint with. Giving the binary its own probe means a container
// HEALTHCHECK can actually check health, instead of only proving the file is
// executable.
//
//	splash -check 127.0.0.1:8080
func probeHealth(addr string, stderr io.Writer) int {
	const timeout = 3 * time.Second

	url := "http://" + strings.TrimPrefix(addr, "http://") + "/api/v1/health/ready"

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Fprintf(stderr, "splash: build probe request: %v\n", err)
		return exitRuntime
	}
	req.Header.Set("User-Agent", version.Get().UserAgent(version.RepoURL))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(stderr, "splash: probe %s failed: %v\n", url, err)
		return exitRuntime
	}
	defer func() { _ = resp.Body.Close() }()

	// Readiness, not liveness: a container that cannot serve traffic should be
	// reported unhealthy even while the process itself is alive.
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "splash: probe %s returned %d\n", url, resp.StatusCode)
		return exitRuntime
	}
	return exitOK
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// indent prefixes every line so a multi-line error reads as one block under the
// heading that introduced it.
func indent(s string) string {
	const prefix = "  "
	out := make([]byte, 0, len(s)+8)
	out = append(out, prefix...)
	for i := 0; i < len(s); i++ {
		out = append(out, s[i])
		if s[i] == '\n' {
			out = append(out, prefix...)
		}
	}
	return string(out)
}
