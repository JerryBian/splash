package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "encoding/json/v2"
)

func TestErrorEnvelopeShape(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/items/missing", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "req-42"))

	WriteError(rec, req, Errorf(http.StatusNotFound, CodeNotFound, "no such item"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if got := rec.Header().Get("X-Request-ID"); got != "" {
		t.Errorf("WriteError should not set the response header itself, got %q", got)
	}

	var envelope struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"requestId"`
			Details   []struct {
				Field   string `json:"field"`
				Message string `json:"message"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.UnmarshalRead(rec.Body, &envelope); err != nil {
		t.Fatalf("body is not the documented envelope: %v\n%s", err, rec.Body.String())
	}
	if envelope.Error.Code != CodeNotFound {
		t.Errorf("code = %q", envelope.Error.Code)
	}
	if envelope.Error.Message != "no such item" {
		t.Errorf("message = %q", envelope.Error.Message)
	}
	if envelope.Error.RequestID != "req-42" {
		t.Errorf("requestId = %q, want the value from the request context", envelope.Error.RequestID)
	}
	if len(envelope.Error.Details) != 0 {
		t.Errorf("details should be omitted when empty: %v", envelope.Error.Details)
	}
}

func TestWriteErrorCarriesFieldDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/items", nil)

	WriteError(rec, req, Errorf(http.StatusUnprocessableEntity, CodeValidationFailed, "rejected").
		WithDetails(
			FieldError{Field: "title", Message: "is required"},
			FieldError{Field: "tags[0]", Message: "must not be empty"},
		))

	body := rec.Body.String()
	for _, want := range []string{`"field":"title"`, `"field":"tags[0]"`, "is required"} {
		if !strings.Contains(body, want) {
			t.Errorf("body is missing %s:\n%s", want, body)
		}
	}
}

func TestWriteErrorHidesInternalDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)

	cause := errors.New("dial tcp 10.0.0.5:5432: connect: connection refused")
	WriteError(rec, req, Errorf(http.StatusInternalServerError, CodeInternal, "%v", cause).WithErr(cause))

	body := rec.Body.String()
	if strings.Contains(body, "10.0.0.5") {
		t.Errorf("a 5xx response leaked internal detail: %s", body)
	}
	if !strings.Contains(body, CodeInternal) {
		t.Errorf("the stable code should still be reported: %s", body)
	}
	if !strings.Contains(body, "request id") {
		t.Errorf("a 5xx should point the reader at the request id: %s", body)
	}
}

func TestWriteErrorFillsDefaults(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	WriteError(rec, req, nil)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for a nil error", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), CodeInternal) {
		t.Errorf("body = %s, want the internal code", rec.Body.String())
	}
}

func TestErrorUnwrapsToItsCause(t *testing.T) {
	cause := errors.New("upstream timed out")
	err := Errorf(http.StatusBadGateway, CodeTimeout, "upstream").WithErr(cause)

	if !errors.Is(err, cause) {
		t.Error("errors.Is could not reach the wrapped cause")
	}
	if got := err.Error(); !strings.Contains(got, CodeTimeout) || !strings.Contains(got, "upstream timed out") {
		t.Errorf("Error() = %q", got)
	}
}

func TestErrorStringWithoutCause(t *testing.T) {
	err := Errorf(http.StatusNotFound, CodeNotFound, "gone")
	if got, want := err.Error(), "httpx: not_found: gone"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestErrorFallsBackToTheStatusText(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	WriteError(rec, req, &Error{Status: http.StatusTeapot, Code: "teapot"})

	if !strings.Contains(rec.Body.String(), "I'm a teapot") {
		t.Errorf("body = %s, want the standard status text as the message", rec.Body.String())
	}
}
