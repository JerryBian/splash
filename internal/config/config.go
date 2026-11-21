// Package config resolves runtime configuration from the environment.
//
// Every setting has a working default, so `go run ./cmd/server` needs no setup
// at all. Deployments override the interesting parts with the SPLASH_*
// variables documented on [Config].
//
// Configuration is read once at startup and then treated as immutable; nothing
// in this package reaches back into the environment later.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Environment selects the behaviour that differs between a laptop and a
// deployed service.
type Environment string

// Supported environments.
const (
	Development Environment = "development"
	Test        Environment = "test"
	Production  Environment = "production"
)

// LogFormat selects the encoding of log records.
type LogFormat string

// Supported log formats.
const (
	// LogFormatText is line-oriented and readable in a terminal.
	LogFormatText LogFormat = "text"
	// LogFormatJSON is what log aggregators want.
	LogFormatJSON LogFormat = "json"
)

// Environment variable names. Declaring them once keeps the configuration
// surface greppable from a single place.
const (
	VarEnvironment         = "SPLASH_ENV"
	VarHTTPAddr            = "SPLASH_HTTP_ADDR"
	VarReadTimeout         = "SPLASH_READ_TIMEOUT"
	VarReadHeaderTimeout   = "SPLASH_READ_HEADER_TIMEOUT"
	VarWriteTimeout        = "SPLASH_WRITE_TIMEOUT"
	VarIdleTimeout         = "SPLASH_IDLE_TIMEOUT"
	VarShutdownTimeout     = "SPLASH_SHUTDOWN_TIMEOUT"
	VarMaxRequestBodyBytes = "SPLASH_MAX_REQUEST_BODY_BYTES"
	VarLogFormat           = "SPLASH_LOG_FORMAT"
	VarLogLevel            = "SPLASH_LOG_LEVEL"
	VarLogFile             = "SPLASH_LOG_FILE"
	VarLogSource           = "SPLASH_LOG_SOURCE"
	VarCORSAllowedOrigins  = "SPLASH_CORS_ALLOWED_ORIGINS"
	VarCORSMaxAge          = "SPLASH_CORS_MAX_AGE"
	VarEnablePprof         = "SPLASH_ENABLE_PPROF"
	VarAdminUsername       = "SPLASH_ADMIN_USERNAME"
	VarAdminPassword       = "SPLASH_ADMIN_PASSWORD"
	VarAuthSecret          = "SPLASH_AUTH_SECRET"
	VarDBPath              = "SPLASH_DB_PATH"
	VarCookieSecure        = "SPLASH_COOKIE_SECURE"
	VarSessionTTL          = "SPLASH_SESSION_TTL"
	VarSeedDemo            = "SPLASH_SEED_DEMO"
)

// DefaultAdminUsername and DefaultAdminPassword are the out-of-the-box
// credentials. They exist so `go run ./cmd/server` works with no setup; a
// deployment that keeps them is flagged at startup by [Config.UsingDefaultCredentials].
const (
	DefaultAdminUsername = "admin"
	DefaultAdminPassword = "splash123"
	// DefaultAuthSecret signs session tokens when SPLASH_AUTH_SECRET is unset.
	// It is public knowledge (it lives in this repository), so production boots
	// warn loudly until it is replaced.
	DefaultAuthSecret = "splash-dev-secret-change-me"
)

// Limits enforced by [Config.Validate].
const (
	// MaxAllowedRequestBodyBytes caps how large an upload the service accepts,
	// so a typo cannot turn into an out-of-memory incident.
	MaxAllowedRequestBodyBytes = 32 << 20 // 32 MiB
	// MaxCORSOrigins bounds the allow-list; a longer list is almost certainly
	// a mistake (or should be "*").
	MaxCORSOrigins = 64
)

// Config is the resolved, validated runtime configuration.
type Config struct {
	// Env is the deployment environment. Defaults to [Development].
	Env Environment
	// HTTPAddr is the listen address, e.g. ":8080" or "127.0.0.1:8080".
	// Use ":0" in tests to get an ephemeral port.
	HTTPAddr string

	// ReadTimeout bounds reading the entire request, body included.
	ReadTimeout time.Duration
	// ReadHeaderTimeout bounds reading the request line and headers. It is the
	// one that actually protects against slowloris.
	ReadHeaderTimeout time.Duration
	// WriteTimeout bounds writing the response.
	WriteTimeout time.Duration
	// IdleTimeout is how long a keep-alive connection may stay idle.
	IdleTimeout time.Duration
	// ShutdownTimeout is the grace period for in-flight requests during a
	// rolling restart.
	ShutdownTimeout time.Duration

	// MaxRequestBodyBytes caps JSON request bodies.
	MaxRequestBodyBytes int64

	// LogFormat is text or json. Defaults to json under [Production].
	LogFormat LogFormat
	// LogLevel is the minimum level that gets emitted.
	LogLevel slog.Level
	// LogFile, when set, additionally writes JSON records to that path.
	LogFile string
	// LogSource adds source file and line to every record. Useful in
	// development, noisy in production.
	LogSource bool

	// CORSAllowedOrigins is the browser-origin allow-list. Empty disables CORS
	// headers entirely; "*" allows any origin (without credentials).
	CORSAllowedOrigins []string
	// CORSMaxAge is how long a browser may cache a preflight response. A nil
	// pointer means "do not send Access-Control-Max-Age at all".
	CORSMaxAge *time.Duration

	// EnablePprof mounts /debug/pprof/. Never enable this on a public
	// interface: profiles leak memory contents and are cheap to abuse.
	EnablePprof bool

	// AdminUsername and AdminPassword are the single administrator account.
	// Anonymous visitors need no credentials; they see public projects only.
	AdminUsername string
	AdminPassword string
	// AuthSecret signs session cookies (HMAC-SHA256). Rotating it invalidates
	// every issued session, which is the intended logout-everywhere lever.
	AuthSecret string
	// DBPath is the SQLite database file. Its parent directory is created at
	// startup. ":memory:" keeps everything in process memory, for tests.
	DBPath string
	// CookieSecure marks the session cookie Secure. Enable it when serving
	// over HTTPS; leave it off for plain-HTTP local development.
	CookieSecure bool
	// SessionTTL is the IDLE timeout of a login: activity slides it forward
	// (see auth.Middleware), so it only bites after this much silence. The
	// cookie itself is browser-session scoped: closing the browser always
	// forces a fresh login regardless of this value.
	SessionTTL time.Duration
	// SeedDemo inserts sample categories, projects and history into an empty
	// database at startup, so a first run has something to look at.
	SeedDemo bool
}

// Default returns the configuration used when nothing is set. It is valid on
// its own: [Config.Validate] accepts it unchanged.
func Default() Config {
	return Config{
		Env:                 Development,
		HTTPAddr:            ":8080",
		ReadTimeout:         15 * time.Second,
		ReadHeaderTimeout:   5 * time.Second,
		WriteTimeout:        30 * time.Second,
		IdleTimeout:         90 * time.Second,
		ShutdownTimeout:     20 * time.Second,
		MaxRequestBodyBytes: 1 << 20, // 1 MiB
		LogFormat:           LogFormatText,
		LogLevel:            slog.LevelInfo,
		LogSource:           false,
		EnablePprof:         false,
		AdminUsername:       DefaultAdminUsername,
		AdminPassword:       DefaultAdminPassword,
		AuthSecret:          DefaultAuthSecret,
		DBPath:              "data/splash.db",
		CookieSecure:        false,
		SessionTTL:          24 * time.Hour,
		SeedDemo:            false,
	}
}

// lookupFunc reads a configuration variable. It is a function rather than a
// direct os.Getenv call so tests can supply a fixed map instead of mutating
// the process environment.
type lookupFunc func(key string) (string, bool)

// Load resolves configuration from the process environment.
func Load() (Config, error) {
	return load(os.LookupEnv)
}

// FromMap resolves configuration from a fixed set of values. Tests prefer this
// over [Load] because it is hermetic: no global state, no ordering hazards
// between parallel subtests.
func FromMap(values map[string]string) (Config, error) {
	return load(func(key string) (string, bool) {
		v, ok := values[key]
		return v, ok
	})
}

func load(lookup lookupFunc) (Config, error) {
	cfg := Default()
	var errs []error

	set(&cfg.Env, lookup, VarEnvironment, parseEnvironment, &errs)
	set(&cfg.HTTPAddr, lookup, VarHTTPAddr, parseNonEmptyString, &errs)
	set(&cfg.ReadTimeout, lookup, VarReadTimeout, parseDuration, &errs)
	set(&cfg.ReadHeaderTimeout, lookup, VarReadHeaderTimeout, parseDuration, &errs)
	set(&cfg.WriteTimeout, lookup, VarWriteTimeout, parseDuration, &errs)
	set(&cfg.IdleTimeout, lookup, VarIdleTimeout, parseDuration, &errs)
	set(&cfg.ShutdownTimeout, lookup, VarShutdownTimeout, parseDuration, &errs)
	set(&cfg.MaxRequestBodyBytes, lookup, VarMaxRequestBodyBytes, parsePositiveInt64, &errs)

	logFormatExplicit := set(&cfg.LogFormat, lookup, VarLogFormat, parseLogFormat, &errs)
	set(&cfg.LogLevel, lookup, VarLogLevel, parseLevel, &errs)
	set(&cfg.LogFile, lookup, VarLogFile, parseString, &errs)
	set(&cfg.LogSource, lookup, VarLogSource, parseBool, &errs)
	set(&cfg.CORSAllowedOrigins, lookup, VarCORSAllowedOrigins, parseStringList, &errs)
	set(&cfg.CORSMaxAge, lookup, VarCORSMaxAge, parseOptionalDuration, &errs)
	set(&cfg.EnablePprof, lookup, VarEnablePprof, parseBool, &errs)
	set(&cfg.AdminUsername, lookup, VarAdminUsername, parseNonEmptyString, &errs)
	set(&cfg.AdminPassword, lookup, VarAdminPassword, parseNonEmptyString, &errs)
	set(&cfg.AuthSecret, lookup, VarAuthSecret, parseNonEmptyString, &errs)
	set(&cfg.DBPath, lookup, VarDBPath, parseNonEmptyString, &errs)
	set(&cfg.CookieSecure, lookup, VarCookieSecure, parseBool, &errs)
	set(&cfg.SessionTTL, lookup, VarSessionTTL, parseDuration, &errs)
	set(&cfg.SeedDemo, lookup, VarSeedDemo, parseBool, &errs)

	// Production defaults to structured logs unless the operator asked for
	// something else explicitly. cfg.Env can only be Production if
	// SPLASH_ENV said so, since the default is Development.
	if !logFormatExplicit && cfg.Env == Production {
		cfg.LogFormat = LogFormatJSON
	}

	if err := cfg.Validate(); err != nil {
		errs = append(errs, err)
	}
	return cfg, errors.Join(errs...)
}

// set resolves key into dst, leaving dst at its default when the variable is
// unset or blank. Parse failures are appended to errs rather than returned, so a
// single bad boot reports every problem at once instead of one per restart.
//
// It reports whether the variable was present at all. Callers use that to tell
// "the operator chose this" from "nobody said anything", which is how the
// production log-format default is decided.
//
// This is a package-level function rather than a closure inside load because Go
// does not allow type parameters on function literals — only on declarations.
func set[T any](dst *T, lookup lookupFunc, key string, parse func(string) (T, error), errs *[]error) bool {
	raw, ok := lookup(key)
	if !ok {
		return false
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	v, err := parse(trimmed)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s=%q: %w", key, raw, err))
		return true
	}
	*dst = v
	return true
}

// Validate reports every problem with the configuration at once, joined into a
// single error, so a failed boot explains itself completely.
func (c Config) Validate() error {
	var errs []error

	if c.Env != Development && c.Env != Test && c.Env != Production {
		errs = append(errs, fmt.Errorf("%s: %q is not one of development, test, production", VarEnvironment, c.Env))
	}
	if host, port, err := net.SplitHostPort(c.HTTPAddr); err != nil {
		errs = append(errs, fmt.Errorf("%s: %q must be host:port: %w", VarHTTPAddr, c.HTTPAddr, err))
	} else if port == "" {
		errs = append(errs, fmt.Errorf("%s: %q is missing a port", VarHTTPAddr, c.HTTPAddr))
	} else if host == "*" {
		errs = append(errs, fmt.Errorf("%s: use 0.0.0.0 rather than * to bind every interface", VarHTTPAddr))
	}

	timeouts := []struct {
		name string
		d    time.Duration
	}{
		{VarReadTimeout, c.ReadTimeout},
		{VarReadHeaderTimeout, c.ReadHeaderTimeout},
		{VarWriteTimeout, c.WriteTimeout},
		{VarIdleTimeout, c.IdleTimeout},
		{VarShutdownTimeout, c.ShutdownTimeout},
	}
	for _, t := range timeouts {
		if t.d <= 0 {
			errs = append(errs, fmt.Errorf("%s: must be positive, got %s", t.name, t.d))
		} else if t.d > 24*time.Hour {
			errs = append(errs, fmt.Errorf("%s: %s is longer than 24h, which disables the timeout in practice", t.name, t.d))
		}
	}
	// A read timeout shorter than the header timeout cannot work: the header
	// budget is part of the overall read budget.
	if c.ReadHeaderTimeout > c.ReadTimeout {
		errs = append(errs, fmt.Errorf("%s (%s) must not exceed %s (%s)",
			VarReadHeaderTimeout, c.ReadHeaderTimeout, VarReadTimeout, c.ReadTimeout))
	}

	switch {
	case c.MaxRequestBodyBytes <= 0:
		errs = append(errs, fmt.Errorf("%s: must be positive, got %d", VarMaxRequestBodyBytes, c.MaxRequestBodyBytes))
	case c.MaxRequestBodyBytes > MaxAllowedRequestBodyBytes:
		errs = append(errs, fmt.Errorf("%s: %d exceeds the hard limit of %d",
			VarMaxRequestBodyBytes, c.MaxRequestBodyBytes, MaxAllowedRequestBodyBytes))
	}

	if c.LogFormat != LogFormatText && c.LogFormat != LogFormatJSON {
		errs = append(errs, fmt.Errorf("%s: %q is not one of text, json", VarLogFormat, c.LogFormat))
	}
	if len(c.CORSAllowedOrigins) > MaxCORSOrigins {
		errs = append(errs, fmt.Errorf("%s: %d origins exceeds the limit of %d",
			VarCORSAllowedOrigins, len(c.CORSAllowedOrigins), MaxCORSOrigins))
	}
	if slices.Contains(c.CORSAllowedOrigins, "*") && len(c.CORSAllowedOrigins) > 1 {
		errs = append(errs, fmt.Errorf("%s: %q cannot be combined with explicit origins",
			VarCORSAllowedOrigins, "*"))
	}
	if c.CORSMaxAge != nil && *c.CORSMaxAge < 0 {
		errs = append(errs, fmt.Errorf("%s: must not be negative, got %s", VarCORSMaxAge, *c.CORSMaxAge))
	}

	if c.AdminUsername == "" || len(c.AdminUsername) > 64 {
		errs = append(errs, fmt.Errorf("%s: must be 1..64 characters", VarAdminUsername))
	}
	if len(c.AdminPassword) < 8 || len(c.AdminPassword) > 128 {
		errs = append(errs, fmt.Errorf("%s: must be 8..128 characters", VarAdminPassword))
	}
	if len(c.AuthSecret) < 16 || len(c.AuthSecret) > 256 {
		errs = append(errs, fmt.Errorf("%s: must be 16..256 characters", VarAuthSecret))
	}
	if c.DBPath == "" {
		errs = append(errs, fmt.Errorf("%s: must not be empty", VarDBPath))
	}
	if c.SessionTTL < time.Hour || c.SessionTTL > 30*24*time.Hour {
		errs = append(errs, fmt.Errorf("%s: must be between 1h and 720h, got %s", VarSessionTTL, c.SessionTTL))
	}

	return errors.Join(errs...)
}

// UsingDefaultCredentials reports whether the admin password or the token
// signing secret is still the shipped default. main warns about this at
// startup: both values are public, so a deployment keeping them is open to
// anyone who has read this repository.
func (c Config) UsingDefaultCredentials() bool {
	return c.AdminPassword == DefaultAdminPassword || c.AuthSecret == DefaultAuthSecret
}

// IsProduction reports whether the process believes it is serving real traffic.
func (c Config) IsProduction() bool { return c.Env == Production }

// IsDevelopment reports whether developer conveniences (verbose errors, pprof,
// permissive CORS) are appropriate.
func (c Config) IsDevelopment() bool { return c.Env == Development }

// WildcardCORS reports whether any origin is allowed.
func (c Config) WildcardCORS() bool { return slices.Contains(c.CORSAllowedOrigins, "*") }

// LogValue implements [slog.LogValuer] so the whole configuration can be logged
// as a single structured record at startup.
//
// If a secret is ever added to Config, redact it here rather than logging the
// struct directly: this method is the only place that decides what leaves the
// process in a log line.
func (c Config) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.String("env", string(c.Env)),
		slog.String("httpAddr", c.HTTPAddr),
		slog.String("logFormat", string(c.LogFormat)),
		slog.String("logLevel", c.LogLevel.String()),
		slog.Bool("logSource", c.LogSource),
		slog.Bool("pprof", c.EnablePprof),
		slog.Int64("maxRequestBodyBytes", c.MaxRequestBodyBytes),
		slog.Duration("shutdownTimeout", c.ShutdownTimeout),
		slog.Any("corsAllowedOrigins", c.CORSAllowedOrigins),
		slog.String("dbPath", c.DBPath),
		slog.String("adminUsername", c.AdminUsername),
		slog.Duration("sessionTTL", c.SessionTTL),
		slog.Bool("cookieSecure", c.CookieSecure),
		slog.Bool("seedDemo", c.SeedDemo),
		// AdminPassword and AuthSecret are deliberately absent: this method is
		// the only place that decides what leaves the process in a log line.
	}
	if c.LogFile != "" {
		attrs = append(attrs, slog.String("logFile", c.LogFile))
	}
	if c.CORSMaxAge != nil {
		attrs = append(attrs, slog.Duration("corsMaxAge", *c.CORSMaxAge))
	}
	return slog.GroupValue(attrs...)
}

func (e Environment) String() string { return string(e) }

func (f LogFormat) String() string { return string(f) }

// Parsers. Each one returns a human-readable error naming the expected shape,
// because these strings end up in the operator's face on a failed boot.

func parseString(s string) (string, error) { return s, nil }

func parseNonEmptyString(s string) (string, error) {
	if s == "" {
		return "", errors.New("must not be empty")
	}
	return s, nil
}

func parseBool(s string) (bool, error) {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return false, errors.New("must be one of true, false, 1, 0")
	}
	return v, nil
}

func parseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, errors.New("must be a duration such as 30s, 500ms or 1m30s")
	}
	return d, nil
}

// parseOptionalDuration returns a pointer so callers can distinguish "unset"
// from "zero". It uses the Go 1.26 form of new, which takes an expression.
func parseOptionalDuration(s string) (*time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return nil, errors.New("must be a duration such as 10m")
	}
	return new(d), nil
}

func parsePositiveInt64(s string) (int64, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, errors.New("must be an integer")
	}
	if n <= 0 {
		return 0, fmt.Errorf("must be positive, got %d", n)
	}
	return n, nil
}

func parseEnvironment(s string) (Environment, error) {
	env := Environment(strings.ToLower(s))
	switch env {
	case Development, Test, Production:
		return env, nil
	default:
		return "", fmt.Errorf("must be one of development, test, production")
	}
}

func parseLogFormat(s string) (LogFormat, error) {
	format := LogFormat(strings.ToLower(s))
	switch format {
	case LogFormatText, LogFormatJSON:
		return format, nil
	default:
		return "", errors.New("must be one of text, json")
	}
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, errors.New("must be one of debug, info, warn, error")
	}
}

// parseStringList splits a comma-separated list, drops blanks, removes
// duplicates and sorts the result so configuration comparisons are stable.
func parseStringList(s string) ([]string, error) {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}
