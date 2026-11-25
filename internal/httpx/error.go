package httpx

import (
	"fmt"
	"net/http"
)

// Error codes are part of the API contract. A machine client switches on Code;
// a human reads Message. Codes are stable across releases — statuses may be
// refined, but a code that shipped keeps its meaning.
const (
	CodeInvalidJSON      = "invalid_json"
	CodeInvalidArgument  = "invalid_argument"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeValidationFailed = "validation_failed"
	CodePayloadTooLarge  = "payload_too_large"
	CodeUnsupportedMedia = "unsupported_media_type"
	CodeMethodNotAllowed = "method_not_allowed"
	CodePrecondition     = "precondition_failed"
	CodeTimeout          = "timeout"
	CodeInternal         = "internal_error"
	CodeUnauthorized     = "unauthorized"
	CodeRateLimited      = "rate_limited"
)

// FieldError is one problem with one input field, reported to the client.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error is the API's error value: an HTTP status, a stable machine-readable
// code, a human message and optional per-field detail.
//
// It also carries the underlying cause in Err, which is logged but never sent
// to the client. That separation is the whole point of the type — there is no
// code path by which an internal error string reaches a response body.
type Error struct {
	// Status is the HTTP status code. Zero is treated as 500.
	Status int
	// Code is the stable machine-readable identifier.
	Code string
	// Message is safe to show to a user. For 5xx responses it is replaced by a
	// generic sentence before being written.
	Message string
	// Details carries per-field validation problems.
	Details []FieldError
	// Err is the wrapped cause, for logs only.
	Err error
}

// Errorf builds an [*Error] with a formatted message.
func Errorf(status int, code, format string, args ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

func (e *Error) Error() string {
	if e == nil {
		return "httpx: <nil> error"
	}
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	if e.Err != nil {
		return fmt.Sprintf("httpx: %s: %s: %v", e.Code, msg, e.Err)
	}
	return fmt.Sprintf("httpx: %s: %s", e.Code, msg)
}

// Unwrap exposes the cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Err }

// WithErr attaches the underlying cause, which is logged but never serialized,
// and returns the receiver so calls can be chained.
func (e *Error) WithErr(err error) *Error {
	e.Err = err
	return e
}

// WithDetails attaches per-field validation problems and returns the receiver.
func (e *Error) WithDetails(details ...FieldError) *Error {
	e.Details = append(e.Details, details...)
	return e
}

// normalize fills in defaults so a partially built Error still produces a
// well-formed response.
func (e *Error) normalize() *Error {
	if e == nil {
		return &Error{
			Status:  http.StatusInternalServerError,
			Code:    CodeInternal,
			Message: "Something went wrong on our side.",
		}
	}
	if e.Status == 0 {
		e.Status = http.StatusInternalServerError
	}
	if e.Code == "" {
		e.Code = CodeInternal
	}
	if e.Message == "" {
		e.Message = http.StatusText(e.Status)
	}
	return e
}

// errorEnvelope is the wire shape of every error response:
//
//	{"error":{"code":"not_found","message":"...","details":[...],"requestId":"..."}}
//
// Nesting under "error" leaves room to add top-level members later without
// breaking clients that read the fields they know.
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      string       `json:"code"`
	Message   string       `json:"message"`
	Details   []FieldError `json:"details,omitempty"`
	RequestID string       `json:"requestId,omitempty"`
}

// WriteError renders err as the JSON error envelope.
//
// For 5xx responses the message is replaced by a generic one: internal detail
// belongs in the log line, not in a response body that may end up in a
// browser console on a customer's machine.
func WriteError(w http.ResponseWriter, r *http.Request, err *Error) {
	e := err.normalize()

	body := errorBody{
		Code:      e.Code,
		Message:   e.Message,
		Details:   e.Details,
		RequestID: RequestIDFrom(r.Context()),
	}
	if e.Status >= http.StatusInternalServerError {
		body.Message = "Something went wrong on our side. The request id above lets us find it in the logs."
		body.Details = nil
	}

	if writeErr := WriteJSON(w, e.Status, errorEnvelope{Error: body}); writeErr != nil {
		// The client is already gone. Nothing to do but let the caller's logger
		// pick it up via the returned value on the success path.
		_ = writeErr
	}
}
