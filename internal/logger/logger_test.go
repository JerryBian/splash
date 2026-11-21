package logger

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JerryBian/splash/internal/config"
)

func TestTextFormat(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(Options{Output: &buf, Format: config.LogFormatText, Level: slog.LevelDebug})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Info("hello", slog.String("requestId", "abc123"))

	out := buf.String()
	for _, want := range []string{"level=INFO", "msg=hello", "requestId=abc123", "service=splash"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q does not contain %q", out, want)
		}
	}
	if log.Level() != slog.LevelDebug {
		t.Errorf("Level() = %v, want debug", log.Level())
	}
	if got := log.String(); got != "logger(debug)" {
		t.Errorf("String() = %q", got)
	}
}

func TestJSONFormat(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(Options{Output: &buf, Format: config.LogFormatJSON, Service: "api"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Warn("careful", slog.Int("status", 429))

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if record["level"] != "WARN" {
		t.Errorf("level = %v, want WARN", record["level"])
	}
	if record["msg"] != "careful" {
		t.Errorf("msg = %v", record["msg"])
	}
	if record["service"] != "api" {
		t.Errorf("service = %v, want api", record["service"])
	}
	if record["status"] != float64(429) {
		t.Errorf("status = %v, want 429", record["status"])
	}
	if _, ok := record["time"]; !ok {
		t.Error("record has no timestamp")
	}
}

func TestLevelFiltersRecords(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(Options{Output: &buf, Level: slog.LevelWarn})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Debug("dropped")
	log.Info("dropped")
	log.Warn("kept")

	if strings.Contains(buf.String(), "dropped") {
		t.Errorf("records below the level were emitted:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "kept") {
		t.Errorf("the warning was not emitted:\n%s", buf.String())
	}
}

func TestFileSinkFansOutWithMultiHandler(t *testing.T) {
	// slog.NewMultiHandler (Go 1.26) drives both sinks from one record, so the
	// file always gets JSON whatever the operator asked stdout for.
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "app.log")

	var stdout bytes.Buffer
	log, err := New(Options{
		Output: &stdout,
		Format: config.LogFormatText,
		File:   path,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Info("both sinks", slog.String("k", "v"))
	if err := log.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !strings.Contains(stdout.String(), "msg=\"both sinks\"") {
		t.Errorf("stdout did not get the text record:\n%s", stdout.String())
	}

	fileBody, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(fileBody, &record); err != nil {
		t.Fatalf("file sink is not JSON: %v\n%s", err, fileBody)
	}
	if record["msg"] != "both sinks" {
		t.Errorf("file record msg = %v", record["msg"])
	}
}

func TestCloseIsRepeatableAndOptional(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	log, err := New(Options{Output: io.Discard, File: path})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// A second Close must not panic or report a closed-file error.
	if err := log.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}

	// A logger with no file has nothing to release.
	plain, err := New(Options{Output: io.Discard})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := plain.Close(); err != nil {
		t.Errorf("Close without a file: %v", err)
	}
}

func TestNopDiscardsEverything(t *testing.T) {
	log := Nop()
	log.Error("this goes nowhere")
	log.With(slog.String("service", "test")).Info("and neither does this")

	// The observable contract of Nop is its level: callers that guard expensive
	// attribute construction with Level() will skip the work.
	if log.Level() <= slog.LevelError {
		t.Errorf("Level() = %v, want above error so guards short-circuit", log.Level())
	}
}

func TestFromConfig(t *testing.T) {
	cfg := config.Default()
	cfg.LogFormat = config.LogFormatJSON
	cfg.LogLevel = slog.LevelError

	var buf bytes.Buffer
	log, err := FromConfig(cfg, &buf)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	defer func() { _ = log.Close() }()

	if log.Level() != slog.LevelError {
		t.Errorf("Level() = %v, want error", log.Level())
	}

	log.Error("from config")
	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("FromConfig did not honour LogFormat=json: %v\n%s", err, buf.String())
	}
	if record["msg"] != "from config" {
		t.Errorf("msg = %v", record["msg"])
	}
}

func TestFromConfigDefaultsToStdoutShape(t *testing.T) {
	// A nil output must not panic: it means "the process's stdout".
	log, err := FromConfig(config.Default(), nil)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	defer func() { _ = log.Close() }()
	if log.Level() != slog.LevelInfo {
		t.Errorf("Level() = %v, want info", log.Level())
	}
}

func TestNewRejectsAnUnknownFormat(t *testing.T) {
	if _, err := New(Options{Output: io.Discard, Format: "yaml"}); err == nil {
		t.Fatal("New accepted an unknown format")
	}
}

func TestNewReportsAnUnwritableFile(t *testing.T) {
	// A path whose parent is a regular file cannot be created, on every OS.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	_, err := New(Options{Output: io.Discard, File: filepath.Join(blocker, "app.log")})
	if err == nil {
		t.Fatal("New accepted a log file it cannot open")
	}
	if !strings.Contains(err.Error(), "logger:") {
		t.Errorf("error %q should be attributed to the logger package", err)
	}
}

func TestAddSourceIncludesCallSite(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(Options{Output: &buf, Format: config.LogFormatJSON, AddSource: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Info("with source")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	source, ok := record["source"].(map[string]any)
	if !ok {
		t.Fatalf("record has no source group: %v", record)
	}
	if fn, _ := source["function"].(string); !strings.Contains(fn, "TestAddSourceIncludesCallSite") {
		t.Errorf("source.function = %q", source["function"])
	}
}
