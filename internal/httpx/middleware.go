package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"
)

// Middleware wraps an [http.Handler] with cross-cutting behaviour.
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares so that the first argument ends up outermost:
//
//	Chain(h, A, B) == A(B(h))
//
// A request therefore flows left to right through the list on the way in, and
// back out in reverse — which is the order you want for
// Chain(handler, Recovery, RequestID, Logging): Recovery catches panics raised
// anywhere below it, and Logging sees the request id that RequestID assigned.
func Chain(handler http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

// RequestIDHeader is read from the inbound request (so a caller's trace id
// survives the hop) and echoed back on every response.
const RequestIDHeader = "X-Request-ID"

// maxRequestIDLength bounds how much of a client-supplied header we are willing
// to repeat in logs and responses.
const maxRequestIDLength = 128

// contextKey is unexported and unique to this package, so nothing else can
// collide with the request id in a context.
type contextKey int

const requestIDKey contextKey = iota

// RequestID makes sure every request has an identifier: the caller's if it sent
// a usable one, a freshly generated UUID otherwise. The id is set on the
// response header and in the request context, where logging picks it up.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !isSafeRequestID(id) {
			id = uuid.New().String()
		}
		w.Header().Set(RequestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFrom returns the request identifier, or "" if the middleware is not
// installed.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// isSafeRequestID accepts only characters that cannot be used to forge a log
// line or smuggle a header value. A client-controlled string ends up in logs
// and in a response header, so it is validated rather than trusted.
func isSafeRequestID(s string) bool {
	if s == "" || len(s) > maxRequestIDLength {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

// Logging emits one structured record per request, at a level derived from the
// response status: info below 400, warn for 4xx, error for 5xx.
func Logging(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			attrs := []any{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int("bytes", rec.written),
				slog.Duration("duration", time.Since(start).Round(time.Microsecond)),
				slog.String("remote", r.RemoteAddr),
				slog.String("proto", r.Proto),
			}
			if ua := r.UserAgent(); ua != "" {
				attrs = append(attrs, slog.String("userAgent", ua))
			}
			if id := RequestIDFrom(r.Context()); id != "" {
				attrs = append(attrs, slog.String("requestId", id))
			}

			level := slog.LevelInfo
			switch {
			case rec.status >= http.StatusInternalServerError:
				level = slog.LevelError
			case rec.status >= http.StatusBadRequest:
				level = slog.LevelWarn
			}
			log.Log(r.Context(), level, "request", attrs...)
		})
	}
}

// statusRecorder captures the status and size of a response.
//
// It implements Unwrap so [http.ResponseController] — and the net/http
// optional-interface probes for Flusher, Hijacker and ReaderFrom — see straight
// through it to the real ResponseWriter. Without Unwrap, wrapping a handler
// would silently break streaming and WebSocket upgrades.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	written     int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.written += n
	return n, err
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Recovery turns a panic in a handler into a 500 with the standard error
// envelope, so one bad request cannot take the process down.
//
// [http.ErrAbortHandler] is re-panicked deliberately: that is net/http's own
// signal that a handler aborted on purpose, and the server already handles it
// without logging.
func Recovery(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(recovered)
				}

				log.ErrorContext(r.Context(), "panic recovered in handler",
					slog.Any("panic", recovered),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("requestId", RequestIDFrom(r.Context())),
					slog.String("stack", string(debug.Stack())),
				)
				WriteError(w, r, Errorf(http.StatusInternalServerError, CodeInternal, "panic"))
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// CORSOptions configures the [CORS] middleware.
type CORSOptions struct {
	// AllowedOrigins is the browser-origin allow-list. Empty disables CORS
	// handling entirely; "*" allows any origin.
	AllowedOrigins []string
	// AllowedMethods and AllowedHeaders default to the set this API uses.
	AllowedMethods []string
	AllowedHeaders []string
	// AllowCredentials lets the browser send cookies and Authorization headers
	// cross-origin. It is honoured only for an explicit allow-list: with "*" it
	// is ignored and the credentials header is not sent, because "any origin may
	// act as this user" is not a configuration anyone meant to write.
	AllowCredentials bool
	// MaxAge is how long a browser may cache a preflight. Nil omits the header.
	MaxAge *time.Duration
}

// Default CORS sets, used when the caller does not override them.
var (
	DefaultCORSMethods = []string{
		http.MethodGet, http.MethodHead, http.MethodPost,
		http.MethodPatch, http.MethodPut, http.MethodDelete, http.MethodOptions,
	}
	DefaultCORSHeaders = []string{
		"Accept", "Authorization", "Content-Type", "If-Match", RequestIDHeader,
	}
)

// CORS answers preflight requests and annotates simple requests with the
// headers a browser needs.
//
// A disallowed origin is passed through without CORS headers rather than
// rejected: the browser blocks the response on its own, and a 403 here would
// hide the real reason from the developer console. Non-browser clients, which
// send no Origin header, are unaffected — CORS is a browser-enforcement
// mechanism, not an authorization one.
func CORS(opts CORSOptions) Middleware {
	if len(opts.AllowedOrigins) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	allowAny := slices.Contains(opts.AllowedOrigins, "*")
	methods := strings.Join(orDefaultSlice(opts.AllowedMethods, DefaultCORSMethods), ", ")
	headers := strings.Join(orDefaultSlice(opts.AllowedHeaders, DefaultCORSHeaders), ", ")
	credentials := opts.AllowCredentials && !allowAny

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			if !allowAny && !slices.Contains(opts.AllowedOrigins, origin) {
				next.ServeHTTP(w, r)
				return
			}

			header := w.Header()
			if allowAny && !opts.AllowCredentials {
				header.Set("Access-Control-Allow-Origin", "*")
			} else {
				header.Set("Access-Control-Allow-Origin", origin)
				// Responses now vary by request header, so caches must be told.
				header.Add("Vary", "Origin")
			}
			if credentials {
				header.Set("Access-Control-Allow-Credentials", "true")
			}
			header.Set("Access-Control-Expose-Headers", "ETag, Location, "+RequestIDHeader)

			isPreflight := r.Method == http.MethodOptions &&
				r.Header.Get("Access-Control-Request-Method") != ""
			if isPreflight {
				header.Set("Access-Control-Allow-Methods", methods)
				header.Set("Access-Control-Allow-Headers", headers)
				if opts.MaxAge != nil {
					header.Set("Access-Control-Max-Age", strconv.Itoa(int(opts.MaxAge.Seconds())))
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// DefaultContentSecurityPolicy is written for a Vite production build served by
// this process: the bundle is a separate hashed file, so no inline script is
// needed, while inline style attributes (which React sets through the DOM)
// still work.
const DefaultContentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"manifest-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// SecurityHeaders sets the response headers that are correct for every request.
// They are applied to the whole mux, API and frontend alike.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		header.Set("Content-Security-Policy", DefaultContentSecurityPolicy)
		header.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		header.Set("Cross-Origin-Opener-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// StrictTransportSecurity pins HTTPS for a year, subdomains included. Mount
// it only in production: over plain HTTP browsers ignore the header, but
// sending it from a dev server would be noise in every response.
func StrictTransportSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// NoCache marks responses as uncacheable. Applied to the API subtree, where a
// stale response is a bug, and deliberately not applied to the frontend's
// hashed assets, which are immutable and should be cached forever.
func NoCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		next.ServeHTTP(w, r)
	})
}

func orDefaultSlice(v, fallback []string) []string {
	if len(v) == 0 {
		return fallback
	}
	return v
}
