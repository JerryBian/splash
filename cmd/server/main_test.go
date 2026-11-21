package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JerryBian/splash/internal/web"
)

// tempDBArgs points a test server at a throwaway database so tests never
// create or share the repository's data/splash.db.
func tempDBArgs(t *testing.T, extra ...string) []string {
	t.Helper()
	return append([]string{"-db-path", filepath.Join(t.TempDir(), "test.db")}, extra...)
}

// syncBuffer is safe for run's goroutine to write while the test reads, which
// `go test -race` would otherwise flag on a plain bytes.Buffer.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor polls a buffer for a substring. The server writes its logs from
// another goroutine, so there is no synchronization point to hang a assertion
// off; polling with a deadline is the honest way to wait for a line.
func waitFor(buf *syncBuffer, needle string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), needle) {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return strings.Contains(buf.String(), needle)
}

func TestVersionFlagPrintsBuildMetadata(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"-version"}, &stdout, &stderr)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exitOK, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"splash", "commit:", "built:", "toolchain:", "platform:", "repository:"} {
		if !strings.Contains(out, want) {
			t.Errorf("--version output is missing %q:\n%s", want, out)
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("--version wrote to stderr:\n%s", stderr.String())
	}
}

func TestHelpExitsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"-h"}, &stdout, &stderr)

	if code != exitOK {
		t.Errorf("exit code = %d, want %d for -h", code, exitOK)
	}
	out := stderr.String()
	for _, want := range []string{"Usage:", "-http-addr", "-log-format", "-pprof", "README.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("help output is missing %q:\n%s", want, out)
		}
	}
}

func TestUnknownFlagExitsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"-nonsense"}, &stdout, &stderr)

	if code != exitUsage {
		t.Errorf("exit code = %d, want %d", code, exitUsage)
	}
}

func TestUnexpectedArgumentExitsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run(context.Background(), []string{"serve"}, &stdout, &stderr)

	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), `unexpected argument "serve"`) {
		t.Errorf("stderr = %q, want it to name the offending argument", stderr.String())
	}
}

func TestBadFlagValueExitsUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "log level", args: []string{"-log-level", "shout"}, want: "-log-level"},
		{name: "log format", args: []string{"-log-format", "yaml"}, want: "-log-format"},
		{name: "http address", args: []string{"-http-addr", "not-an-address"}, want: "SPLASH_HTTP_ADDR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run(context.Background(), tt.args, &stdout, &stderr)

			if code != exitUsage {
				t.Fatalf("exit code = %d, want %d", code, exitUsage)
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("stderr does not mention %q:\n%s", tt.want, stderr.String())
			}
		})
	}
}

func TestBadEnvironmentVariableExitsUsage(t *testing.T) {
	// t.Setenv restores the previous value, so the process environment is not
	// left dirty for the tests that follow.
	t.Setenv("SPLASH_ENV", "staging")

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), nil, &stdout, &stderr)

	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "SPLASH_ENV") {
		t.Errorf("stderr does not name the offending variable:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "README.md") {
		t.Errorf("a configuration failure should point at the documentation:\n%s", stderr.String())
	}
}

func TestRunStartsServesAndStopsCleanly(t *testing.T) {
	var stdout, stderr syncBuffer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	code := make(chan int, 1)
	go func() {
		code <- run(ctx, tempDBArgs(t,
			"-http-addr", "127.0.0.1:0",
			"-log-format", "json",
		), &stdout, &stderr)
	}()

	if !waitFor(&stdout, `"msg":"listening"`, 10*time.Second) {
		t.Fatalf("the server never logged that it was listening\nstdout: %s\nstderr: %s",
			stdout.String(), stderr.String())
	}
	// The configuration is logged at startup so an operator can confirm which
	// environment and address the process actually loaded.
	if !waitFor(&stdout, `"config"`, 2*time.Second) {
		t.Errorf("startup did not log the resolved configuration\nstdout: %s", stdout.String())
	}

	cancel()

	select {
	case got := <-code:
		if got != exitOK {
			t.Errorf("exit code = %d, want %d\nstdout: %s\nstderr: %s",
				got, exitOK, stdout.String(), stderr.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("run did not return after the context was canceled")
	}

	for _, want := range []string{`"msg":"shutting down"`, `"msg":"stopped cleanly"`} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("the log is missing %s\nstdout: %s", want, stdout.String())
		}
	}
}

func TestCheckFlagSucceedsAgainstAHealthyInstance(t *testing.T) {
	var stdout, stderr syncBuffer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	code := make(chan int, 1)
	go func() {
		code <- run(ctx, tempDBArgs(t, "-http-addr", "127.0.0.1:0", "-log-format", "json"), &stdout, &stderr)
	}()

	if !waitFor(&stdout, `"msg":"listening"`, 10*time.Second) {
		t.Fatalf("the server never started\nstdout: %s\nstderr: %s", stdout.String(), stderr.String())
	}
	addr := listeningAddr(t, stdout.String())

	// -check exists so a distroless container can have a real HEALTHCHECK: no
	// shell and no curl means the binary has to probe itself.
	var probe bytes.Buffer
	if got := run(context.Background(), []string{"-check", addr}, &probe, &probe); got != exitOK {
		t.Errorf("-check %s = %d, want %d\n%s", addr, got, exitOK, probe.String())
	}

	cancel()
	if got := <-code; got != exitOK {
		t.Errorf("server exit code = %d", got)
	}
}

func TestCheckFlagFailsWhenNothingIsListening(t *testing.T) {
	var out bytes.Buffer

	// Nothing listens on loopback port 1, so the probe must fail rather than
	// hang or report success.
	got := run(context.Background(), []string{"-check", "127.0.0.1:1"}, &out, &out)

	if got != exitRuntime {
		t.Errorf("-check exit code = %d, want %d", got, exitRuntime)
	}
	if !strings.Contains(out.String(), "probe") {
		t.Errorf("stderr = %q, want it to name the probe", out.String())
	}
}

// listeningAddr pulls the resolved address out of the startup log line, which is
// the only way to learn the port when the server was asked for port 0.
func listeningAddr(t *testing.T, logs string) string {
	t.Helper()
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, `"msg":"listening"`) {
			continue
		}
		var record struct {
			Addr string `json:"addr"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("could not parse the listening log line %q: %v", line, err)
		}
		if record.Addr != "" {
			return record.Addr
		}
	}
	t.Fatalf("no listening log line with an address in:\n%s", logs)
	return ""
}

func TestRunMentionsTheMissingFrontendBuild(t *testing.T) {
	var stdout, stderr syncBuffer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	code := make(chan int, 1)
	go func() {
		code <- run(ctx, tempDBArgs(t, "-http-addr", "127.0.0.1:0"), &stdout, &stderr)
	}()

	if !waitFor(&stdout, "listening", 10*time.Second) {
		t.Fatalf("the server never started\nstdout: %s\nstderr: %s", stdout.String(), stderr.String())
	}
	cancel()
	<-code

	// Whether the warning appears depends on whether the frontend has been
	// built in this working copy, so assert the pairing rather than the warning
	// alone: if it is absent, the build must be embedded.
	logged := stdout.String()
	hasWarning := strings.Contains(logged, "no frontend build is embedded")
	if hasWarning == web.Built() {
		t.Errorf("warning present = %v but web.Built() = %v; they must disagree\nstdout: %s",
			hasWarning, web.Built(), logged)
	}
}

func TestSeedFlagPopulatesAnEmptyDatabase(t *testing.T) {
	var stdout, stderr syncBuffer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	code := make(chan int, 1)
	go func() {
		code <- run(ctx, tempDBArgs(t, "-http-addr", "127.0.0.1:0", "-log-format", "json", "-seed"), &stdout, &stderr)
	}()

	if !waitFor(&stdout, `"msg":"listening"`, 10*time.Second) {
		t.Fatalf("the server never started\nstdout: %s\nstderr: %s", stdout.String(), stderr.String())
	}
	addr := listeningAddr(t, stdout.String())

	resp, err := http.Get("http://" + addr + "/api/v1/projects")
	if err != nil {
		t.Fatalf("GET projects: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"name"`) {
		t.Errorf("seeded projects: status %d, body %s", resp.StatusCode, body)
	}
	if !strings.Contains(stdout.String(), "seeded demo data") {
		t.Errorf("the seed was not logged:\n%s", stdout.String())
	}

	cancel()
	if got := <-code; got != exitOK {
		t.Errorf("exit code = %d", got)
	}
}

func TestDefaultCredentialsWarnAtStartup(t *testing.T) {
	var stdout, stderr syncBuffer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	code := make(chan int, 1)
	go func() {
		code <- run(ctx, tempDBArgs(t, "-http-addr", "127.0.0.1:0", "-log-format", "json"), &stdout, &stderr)
	}()
	if !waitFor(&stdout, `"msg":"listening"`, 10*time.Second) {
		t.Fatalf("the server never started\nstdout: %s", stdout.String())
	}
	cancel()
	<-code

	if !strings.Contains(stdout.String(), "default admin password or auth secret") {
		t.Errorf("keeping the shipped credentials must warn at startup:\n%s", stdout.String())
	}
}
