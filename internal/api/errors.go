package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/JerryBian/splash/internal/httpx"
	"github.com/JerryBian/splash/internal/store"
)

// fail is the single error exit for every handler: map the error to a status,
// log it at a level that matches, and write the JSON envelope.
//
// Routing every failure through one function is what makes it impossible for a
// new handler to forget logging, or to leak an internal message to a client.
func (a *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	apiErr := a.mapError(err)

	if apiErr.Status >= http.StatusInternalServerError {
		a.log.ErrorContext(r.Context(), "request failed",
			slog.String("requestId", httpx.RequestIDFrom(r.Context())),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("code", apiErr.Code),
			slog.Any("error", err),
		)
	} else {
		// 4xx responses are the client's fault and expected in normal traffic,
		// so they are logged quietly and without the cause chain.
		a.log.DebugContext(r.Context(), "request rejected",
			slog.String("requestId", httpx.RequestIDFrom(r.Context())),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", apiErr.Status),
			slog.String("code", apiErr.Code),
		)
	}

	httpx.WriteError(w, r, apiErr)
}

// mapError translates a domain or transport error into the API's error value.
//
// The order of the checks matters: the most specific types first, because
// errors.Is(err, store.ErrValidation) also matches a ValidationErrors value,
// and only the typed branch can report per-field detail.
func (a *API) mapError(err error) *httpx.Error {
	if err == nil {
		return httpx.Errorf(http.StatusInternalServerError, httpx.CodeInternal, "no error to report")
	}

	// Already an HTTP error: the handler decided the status itself.
	var apiErr *httpx.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}

	if httpx.IsPayloadTooLarge(err) {
		return httpx.Errorf(http.StatusRequestEntityTooLarge, httpx.CodePayloadTooLarge,
			"request body exceeds the %d byte limit", a.maxBody).WithErr(err)
	}
	if httpx.IsMalformedJSON(err) {
		return httpx.Errorf(http.StatusBadRequest, httpx.CodeInvalidJSON,
			"request body is not valid JSON").WithErr(err)
	}

	var problems store.ValidationErrors
	if errors.As(err, &problems) {
		details := make([]httpx.FieldError, len(problems))
		for i, p := range problems {
			details[i] = httpx.FieldError{Field: p.Field, Message: p.Message}
		}
		return httpx.Errorf(http.StatusUnprocessableEntity, httpx.CodeValidationFailed,
			"the request was rejected: %d field problem(s)", len(problems)).
			WithDetails(details...).WithErr(err)
	}
	if errors.Is(err, store.ErrValidation) {
		return httpx.Errorf(http.StatusUnprocessableEntity, httpx.CodeValidationFailed,
			"the request was rejected").WithErr(err)
	}

	if errors.Is(err, store.ErrNotFound) {
		return httpx.Errorf(http.StatusNotFound, httpx.CodeNotFound,
			"the requested resource does not exist").WithErr(err)
	}
	// Version conflict is checked before the generic conflict: an If-Match
	// failure is a 412, while a name collision or a lost race is a 409.
	if errors.Is(err, store.ErrVersionConflict) {
		return httpx.Errorf(http.StatusPreconditionFailed, httpx.CodePrecondition,
			"the project changed since you read it; fetch it again before writing").WithErr(err)
	}
	if errors.Is(err, store.ErrConflict) {
		return httpx.Errorf(http.StatusConflict, httpx.CodeConflict,
			"the request conflicts with the current state").WithErr(err)
	}

	var numErr *strconv.NumError
	if errors.As(err, &numErr) {
		return httpx.Errorf(http.StatusBadRequest, httpx.CodeInvalidArgument,
			"%q is not a valid number", numErr.Num).WithErr(err)
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return httpx.Errorf(http.StatusGatewayTimeout, httpx.CodeTimeout,
			"the request timed out").WithErr(err)
	case errors.Is(err, context.Canceled):
		// The client went away. There is nobody to answer, but returning a
		// value keeps the caller's code path uniform.
		return httpx.Errorf(http.StatusBadRequest, httpx.CodeInvalidArgument,
			"the request was canceled").WithErr(err)
	}

	return httpx.Errorf(http.StatusInternalServerError, httpx.CodeInternal,
		"unexpected error").WithErr(err)
}
