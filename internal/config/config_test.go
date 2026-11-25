package config

import (
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDefaultPassesValidation(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Default() must be valid on its own, got: %v", err)
	}
	if cfg.IsProduction() {
		t.Error("Default() should not claim to be production")
	}
	if !cfg.IsDevelopment() {
		t.Error("Default() should be development")
	}
}

func TestLoadWithoutEnvironmentUsesDefaults(t *testing.T) {
	got, err := FromMap(nil)
	if err != nil {
		t.Fatalf("FromMap(nil): %v", err)
	}
	// Config holds a slice, so == is not available on it; DeepEqual is the
	// honest way to assert "nothing changed".
	if want := Default(); !reflect.DeepEqual(got, want) {
		t.Errorf("FromMap(nil) = %+v, want %+v", got, want)
	}
}

func TestFromMapParsesEveryVariable(t *testing.T) {
	values := map[string]string{
		VarEnvironment:         "Production", // case-insensitive
		VarHTTPAddr:            "127.0.0.1:9000",
		VarReadTimeout:         "3s",
		VarReadHeaderTimeout:   "1s",
		VarWriteTimeout:        "6s",
		VarIdleTimeout:         "30s",
		VarShutdownTimeout:     "9s",
		VarMaxRequestBodyBytes: "2048",
		VarLogFormat:           "text",
		VarLogLevel:            "warn",
		VarLogFile:             " /tmp/splash.log ", // surrounding space is trimmed
		VarLogSource:           "true",
		VarCORSAllowedOrigins:  "https://b.example, https://a.example,https://a.example",
		VarCORSMaxAge:          "15m",
		VarEnablePprof:         "1",
	}

	cfg, err := FromMap(values)
	if err != nil {
		t.Fatalf("FromMap: %v", err)
	}

	if cfg.Env != Production {
		t.Errorf("Env = %q, want production", cfg.Env)
	}
	if cfg.HTTPAddr != "127.0.0.1:9000" {
		t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if cfg.ReadTimeout != 3*time.Second {
		t.Errorf("ReadTimeout = %s", cfg.ReadTimeout)
	}
	if cfg.MaxRequestBodyBytes != 2048 {
		t.Errorf("MaxRequestBodyBytes = %d", cfg.MaxRequestBodyBytes)
	}
	// An explicit text format must survive the production default, otherwise
	// the operator's choice is silently overruled.
	if cfg.LogFormat != LogFormatText {
		t.Errorf("LogFormat = %q, want text", cfg.LogFormat)
	}
	if cfg.LogLevel != slog.LevelWarn {
		t.Errorf("LogLevel = %v, want warn", cfg.LogLevel)
	}
	if cfg.LogFile != "/tmp/splash.log" {
		t.Errorf("LogFile = %q, want trimmed value", cfg.LogFile)
	}
	if !cfg.LogSource {
		t.Error("LogSource = false, want true")
	}
	want := []string{"https://a.example", "https://b.example"}
	if strings.Join(cfg.CORSAllowedOrigins, ",") != strings.Join(want, ",") {
		t.Errorf("CORSAllowedOrigins = %v, want %v sorted and de-duplicated", cfg.CORSAllowedOrigins, want)
	}
	if cfg.CORSMaxAge == nil || *cfg.CORSMaxAge != 15*time.Minute {
		t.Errorf("CORSMaxAge = %v, want 15m", cfg.CORSMaxAge)
	}
	if !cfg.EnablePprof {
		t.Error("EnablePprof = false, want true")
	}
	if !cfg.IsProduction() {
		t.Error("IsProduction() = false after SPLASH_ENV=Production")
	}
}

func TestProductionDefaultsToJSONLogs(t *testing.T) {
	cfg, err := FromMap(map[string]string{VarEnvironment: "production"})
	if err != nil {
		t.Fatalf("FromMap: %v", err)
	}
	if cfg.LogFormat != LogFormatJSON {
		t.Errorf("LogFormat = %q, want json under production", cfg.LogFormat)
	}
}

func TestBlankVariableFallsBackToTheDefault(t *testing.T) {
	cfg, err := FromMap(map[string]string{VarHTTPAddr: "   ", VarLogLevel: ""})
	if err != nil {
		t.Fatalf("FromMap: %v", err)
	}
	if cfg.HTTPAddr != Default().HTTPAddr {
		t.Errorf("HTTPAddr = %q, want the default", cfg.HTTPAddr)
	}
	if cfg.LogLevel != Default().LogLevel {
		t.Errorf("LogLevel = %v, want the default", cfg.LogLevel)
	}
}

func TestCORSMaxAgeUsesAnExpressionPointer(t *testing.T) {
	// new(expr) is Go 1.26 syntax: it allocates and initialises in one step,
	// which is what an optional duration field wants. Before 1.26 this needed a
	// temporary variable or a helper function.
	cfg := Default()
	if cfg.CORSMaxAge != nil {
		t.Fatal("Default() should leave CORSMaxAge unset so the header is omitted")
	}
	cfg.CORSMaxAge = new(10 * time.Minute)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if *cfg.CORSMaxAge != 10*time.Minute {
		t.Errorf("CORSMaxAge = %s, want 10m", *cfg.CORSMaxAge)
	}
}

func TestInvalidValuesAreAllReportedAtOnce(t *testing.T) {
	_, err := FromMap(map[string]string{
		VarEnvironment:         "staging",
		VarReadTimeout:         "soon",
		VarLogLevel:            "shout",
		VarMaxRequestBodyBytes: "-1",
	})
	if err == nil {
		t.Fatal("expected an error for four bad variables")
	}

	// errors.Join keeps every problem, so one bad deploy explains itself fully
	// instead of failing one variable per restart. A variable that fails to
	// parse keeps its default, so Validate finds nothing extra to add.
	msg := err.Error()
	for _, want := range []string{VarEnvironment, VarReadTimeout, VarLogLevel, VarMaxRequestBodyBytes} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not mention %s:\n%s", want, msg)
		}
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("expected a joined error from errors.Join, got %T", err)
	}
	if n := len(joined.Unwrap()); n != 4 {
		t.Errorf("joined %d errors, want one per bad variable (4)", n)
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantSub string
	}{
		{
			name:    "address without a port",
			mutate:  func(c *Config) { c.HTTPAddr = "localhost" },
			wantSub: VarHTTPAddr,
		},
		{
			name:    "star host",
			mutate:  func(c *Config) { c.HTTPAddr = "*:8080" },
			wantSub: "0.0.0.0",
		},
		{
			name:    "header timeout longer than read timeout",
			mutate:  func(c *Config) { c.ReadHeaderTimeout = 30 * time.Second },
			wantSub: VarReadHeaderTimeout,
		},
		{
			name:    "write timeout longer than a day",
			mutate:  func(c *Config) { c.WriteTimeout = 48 * time.Hour },
			wantSub: "24h",
		},
		{
			name:    "body limit above the hard cap",
			mutate:  func(c *Config) { c.MaxRequestBodyBytes = MaxAllowedRequestBodyBytes + 1 },
			wantSub: "hard limit",
		},
		{
			name:    "unknown log format",
			mutate:  func(c *Config) { c.LogFormat = "yaml" },
			wantSub: VarLogFormat,
		},
		{
			name:    "wildcard mixed with explicit origins",
			mutate:  func(c *Config) { c.CORSAllowedOrigins = []string{"*", "https://a.example"} },
			wantSub: VarCORSAllowedOrigins,
		},
		{
			name:    "negative CORS max age",
			mutate:  func(c *Config) { c.CORSMaxAge = new(-time.Minute) },
			wantSub: VarCORSMaxAge,
		},
		{
			name:    "negative shutdown timeout",
			mutate:  func(c *Config) { c.ShutdownTimeout = -1 },
			wantSub: VarShutdownTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("Validate() = %q, want it to mention %q", err, tt.wantSub)
			}
		})
	}
}

func TestParseStringList(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  ,  ", []string{}},
		{"b,a", []string{"a", "b"}},
		{"a,a,b", []string{"a", "b"}},
		{" https://x.example , https://y.example ", []string{"https://x.example", "https://y.example"}},
	}
	for _, tt := range tests {
		got, err := parseStringList(tt.in)
		if err != nil {
			t.Fatalf("parseStringList(%q): %v", tt.in, err)
		}
		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("parseStringList(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestLogValueIsAStructuredGroup(t *testing.T) {
	cfg := Default()
	cfg.CORSMaxAge = new(5 * time.Minute)

	value := cfg.LogValue()
	if value.Kind() != slog.KindGroup {
		t.Fatalf("LogValue kind = %v, want a group", value.Kind())
	}

	found := map[string]bool{}
	for _, attr := range value.Group() {
		found[attr.Key] = true
	}
	for _, want := range []string{"env", "httpAddr", "logFormat", "logLevel", "corsMaxAge"} {
		if !found[want] {
			t.Errorf("LogValue is missing %q; got keys %v", want, found)
		}
	}
}

func TestApplyOverrides(t *testing.T) {
	cfg := Default()
	pprof := true

	err := cfg.Apply(Overrides{
		HTTPAddr:    ":9999",
		LogFormat:   "json",
		LogLevel:    "debug",
		EnablePprof: &pprof,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if cfg.HTTPAddr != ":9999" || cfg.LogFormat != LogFormatJSON || cfg.LogLevel != slog.LevelDebug {
		t.Errorf("Apply did not take effect: %+v", cfg)
	}
	if !cfg.EnablePprof {
		t.Error("EnablePprof = false, want the flag value")
	}
}

func TestApplyOverridesLeavesUnsetFlagsAlone(t *testing.T) {
	cfg := Default()
	cfg.EnablePprof = true

	// A false bool flag that was never passed must not undo the environment.
	if err := cfg.Apply(Overrides{}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !cfg.EnablePprof {
		t.Error("an unspecified -pprof flag overrode the environment value")
	}
	if cfg.HTTPAddr != Default().HTTPAddr {
		t.Errorf("HTTPAddr changed to %q", cfg.HTTPAddr)
	}
}

func TestApplyOverridesRejectsBadFlagValues(t *testing.T) {
	cfg := Default()
	err := cfg.Apply(Overrides{LogLevel: "shout", LogFormat: "yaml"})
	if err == nil {
		t.Fatal("Apply accepted invalid flag values")
	}
	for _, want := range []string{"-log-level", "-log-format"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not name %s: %v", want, err)
		}
	}
}

func TestCookieSecureDefaultsByEnvironment(t *testing.T) {
	// Development leaves the cookie non-Secure for plain-HTTP local use.
	dev, err := FromMap(map[string]string{})
	if err != nil {
		t.Fatalf("dev config: %v", err)
	}
	if dev.CookieSecure {
		t.Error("development should default CookieSecure to false")
	}
	// Production defaults it to True (HTTPS expected).
	prod, err := FromMap(map[string]string{"SPLASH_ENV": "production"})
	if err != nil {
		t.Fatalf("prod config: %v", err)
	}
	if !prod.CookieSecure {
		t.Error("production should default CookieSecure to true")
	}
	// An explicit value wins over the production default.
	override, err := FromMap(map[string]string{"SPLASH_ENV": "production", "SPLASH_COOKIE_SECURE": "false"})
	if err != nil {
		t.Fatalf("override config: %v", err)
	}
	if override.CookieSecure {
		t.Error("explicit SPLASH_COOKIE_SECURE=false should override the production default")
	}
}
