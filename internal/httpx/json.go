// Package httpx holds the transport plumbing shared by every HTTP handler:
// JSON encoding, the error envelope and middleware.
//
// JSON goes through encoding/json/v2, which became part of the standard
// library in Go 1.27. Compared with encoding/json it is stricter and friendlier
// to a TypeScript client:
//
//   - a nil slice encodes as [], not null, so the frontend never needs a
//     `?? []` guard;
//   - invalid UTF-8 and duplicate object members are rejected rather than
//     silently rewritten;
//   - unmarshalling is significantly faster.
package httpx

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"
)

// ErrUnsupportedMediaType reports a request body that is not declared as JSON.
// Handlers map it to 415.
var ErrUnsupportedMediaType = errors.New("httpx: request body must be application/json")

// IsUnsupportedMediaType reports whether err is the content-type rejection.
func IsUnsupportedMediaType(err error) bool { return errors.Is(err, ErrUnsupportedMediaType) }

// isJSONContentType accepts "application/json" with optional parameters
// (charset, boundary). Requiring it is CSRF defence in depth: a cross-site
// form POST cannot set this content type without triggering a CORS preflight,
// so only same-origin JSON requests (or non-browser clients) reach a handler.
func isJSONContentType(header string) bool {
	mediaType, _, err := mime.ParseMediaType(header)
	return err == nil && mediaType == "application/json"
}

// Content types used across the API. Only the two the service actually sends
// are declared: an unused constant is an invitation to guess at a convention
// that does not exist.
const (
	ContentTypeJSON = "application/json; charset=utf-8"
	ContentTypeHTML = "text/html; charset=utf-8"
)

// WriteJSON encodes v as the response body with the given status.
//
// Encoding happens into a buffer first: once the status line and headers are on
// the wire they cannot be changed, so discovering an encoding failure halfway
// through would leave the client holding a truncated 200. Returning the error
// lets the caller log it; there is nothing useful left to tell the client.
func WriteJSON(w http.ResponseWriter, status int, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("httpx: encode response: %w", err)
	}
	body = append(body, '\n')

	header := w.Header()
	header.Set("Content-Type", ContentTypeJSON)
	header.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)

	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("httpx: write response: %w", err)
	}
	return nil
}

// Write sends a pre-rendered body. It is what the SPA handler and the health
// endpoint use, where the payload is not a Go value.
func Write(w http.ResponseWriter, status int, contentType string, body []byte) {
	header := w.Header()
	header.Set("Content-Type", contentType)
	header.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// DecodeJSON reads and decodes a request body into T.
//
// The body is capped with [http.MaxBytesReader] so a large or endless upload is
// stopped before it is buffered, and the resulting [*http.MaxBytesError] is
// returned unchanged for the caller to map onto 413.
//
// Because it uses encoding/json/v2, duplicate object members and invalid UTF-8
// are rejected with a typed error rather than quietly accepted.
func DecodeJSON[T any](w http.ResponseWriter, r *http.Request, maxBytes int64) (T, error) {
	var out T
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		return out, ErrUnsupportedMediaType
	}
	body := r.Body
	if maxBytes > 0 {
		body = http.MaxBytesReader(w, r.Body, maxBytes)
	}
	if err := json.UnmarshalRead(body, &out); err != nil {
		return out, err
	}
	return out, nil
}

// IsPayloadTooLarge reports whether err came from exceeding the request body
// limit. Handlers use it to answer 413 instead of a generic 400.
func IsPayloadTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

// IsMalformedJSON reports whether err is a JSON syntax or type problem coming
// from the client rather than a bug in the service.
//
// encoding/json/v2 splits failures into syntactic (the bytes are not JSON) and
// semantic (the JSON is valid but does not fit the Go type). Both mean "the
// client sent something wrong", so they map onto the same 400.
func IsMalformedJSON(err error) bool {
	var syntactic *jsontext.SyntacticError
	if errors.As(err, &syntactic) {
		return true
	}
	var semantic *json.SemanticError
	return errors.As(err, &semantic)
}
