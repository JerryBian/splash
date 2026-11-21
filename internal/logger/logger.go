// Package logger builds the process-wide [slog] logger.
//
// Everything in the codebase takes a *slog.Logger as a dependency; this package
// is the only place that knows how the handler chain is assembled, which keeps
// the rest of the program free of logging policy.
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/JerryBian/splash/internal/config"
)

// Options configures [New].
type Options struct {
	// Level is the minimum level that gets emitted. Zero means Info.
	Level slog.Level
	// Format is text or json. Empty means text.
	Format config.LogFormat
	// AddSource includes the emitting file and line in every record.
	AddSource bool
	// Output is the primary sink. Nil means [os.Stdout].
	Output io.Writer
	// File, when set, is an additional sink that always receives JSON records.
	// Both sinks are driven by a single [slog.MultiHandler] (Go 1.26), so a
	// record is formatted once and fanned out.
	File string
	// Service is attached to every record as the "service" attribute, which is
	// what makes logs from several deployments separable in one aggregator.
	// Empty means "splash".
	Service string
	// Attrs are extra default attributes, e.g. slog.String("deployment", id).
	Attrs []slog.Attr
}

// Logger is a [*slog.Logger] that may own a log file and therefore needs
// closing. It embeds the logger, so every slog method is available directly.
type Logger struct {
	*slog.Logger

	level slog.Level

	file     *os.File
	closeMu  sync.Mutex
	closed   bool
	closeErr error
}

// New builds a logger from opts. The returned Logger must be closed to release
// the log file, if one was configured.
func New(opts Options) (*Logger, error) {
	output := opts.Output
	if output == nil {
		output = os.Stdout
	}
	format := opts.Format
	if format == "" {
		format = config.LogFormatText
	}
	if format != config.LogFormatText && format != config.LogFormatJSON {
		return nil, fmt.Errorf("logger: unsupported format %q", format)
	}

	service := opts.Service
	if service == "" {
		service = "splash"
	}

	handlerOpts := &slog.HandlerOptions{Level: opts.Level, AddSource: opts.AddSource}

	var primary slog.Handler
	if format == config.LogFormatJSON {
		primary = slog.NewJSONHandler(output, handlerOpts)
	} else {
		primary = slog.NewTextHandler(output, handlerOpts)
	}

	handlers := []slog.Handler{primary}
	var file *os.File
	if opts.File != "" {
		if dir := filepath.Dir(opts.File); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return nil, fmt.Errorf("logger: create %s: %w", dir, err)
			}
		}
		f, err := os.OpenFile(opts.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, fmt.Errorf("logger: open %s: %w", opts.File, err)
		}
		file = f
		// The file sink is always JSON: it exists for machines, while stdout
		// follows whatever the operator asked for.
		handlers = append(handlers, slog.NewJSONHandler(file, handlerOpts))
	}

	handler := handlers[0]
	if len(handlers) > 1 {
		handler = slog.NewMultiHandler(handlers...)
	}

	attrs := make([]any, 0, len(opts.Attrs)+1)
	attrs = append(attrs, slog.String("service", service))
	for _, a := range opts.Attrs {
		attrs = append(attrs, a)
	}

	return &Logger{
		Logger: slog.New(handler).With(attrs...),
		level:  opts.Level,
		file:   file,
	}, nil
}

// FromConfig builds the process logger from resolved configuration. A nil
// output means [os.Stdout]; main passes its own writer so the log destination
// stays injectable for tests.
func FromConfig(cfg config.Config, output io.Writer) (*Logger, error) {
	return New(Options{
		Level:     cfg.LogLevel,
		Format:    cfg.LogFormat,
		AddSource: cfg.LogSource,
		File:      cfg.LogFile,
		Output:    output,
	})
}

// Nop returns a logger that discards everything. Tests use it to keep output
// readable without threading nil checks through the code.
func Nop() *Logger {
	return &Logger{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), level: slog.LevelError + 1}
}

// Close releases the log file, if any. It is safe to call more than once and
// safe to call on a logger with no file.
func (l *Logger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.closeMu.Lock()
	defer l.closeMu.Unlock()
	if l.closed {
		return l.closeErr
	}
	l.closed = true
	l.closeErr = l.file.Close()
	return l.closeErr
}

// Level returns the minimum level this logger was built with. slog itself does
// not expose the level of a handler, so the value is remembered at construction.
func (l *Logger) Level() slog.Level { return l.level }

// String describes the logger, used by startup logging and tests.
func (l *Logger) String() string {
	return "logger(" + levelName(l.level) + ")"
}

func levelName(l slog.Level) string {
	switch {
	case l <= slog.LevelDebug:
		return "debug"
	case l <= slog.LevelInfo:
		return "info"
	case l <= slog.LevelWarn:
		return "warn"
	case l <= slog.LevelError:
		return "error"
	default:
		return l.String()
	}
}
