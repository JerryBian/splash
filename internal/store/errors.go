package store

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors returned by the storage layer. The HTTP layer maps them onto
// status codes; nothing else in the program needs to know what a 404 is.
var (
	// ErrNotFound reports that no record exists for the requested identity.
	ErrNotFound = errors.New("store: not found")
	// ErrConflict reports that the write would collide with an existing record.
	ErrConflict = errors.New("store: conflict")
	// ErrValidation is the sentinel matched by [ValidationErrors].
	ErrValidation = errors.New("store: validation failed")
)

// FieldError is a single problem with a single input field.
type FieldError struct {
	// Field is a JSON Pointer-ish path such as "title" or "tags[2]".
	Field string `json:"field"`
	// Message completes the sentence "<field> <message>", e.g. "is required".
	// It is safe to show to an end user.
	Message string `json:"message"`
}

func (e FieldError) String() string { return e.Field + " " + e.Message }

// ValidationErrors collects every problem found in one pass, so a client can
// fix them all at once instead of discovering them one round trip at a time.
//
// It implements [error] directly and matches [ErrValidation] through Is, which
// keeps call sites to a single errors.Is check.
type ValidationErrors []FieldError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ErrValidation.Error()
	}
	parts := make([]string, len(e))
	for i, fe := range e {
		parts[i] = fe.String()
	}
	return fmt.Sprintf("%s: %s", ErrValidation.Error(), strings.Join(parts, "; "))
}

// Is reports whether target is [ErrValidation], so callers can write
// errors.Is(err, store.ErrValidation) without this type wrapping the sentinel.
func (e ValidationErrors) Is(target error) bool { return target == ErrValidation }

// Fields returns the offending field paths, in the order they were found.
func (e ValidationErrors) Fields() []string {
	out := make([]string, len(e))
	for i, fe := range e {
		out[i] = fe.Field
	}
	return out
}

// collect turns a slice of optional field errors into a single error,
// returning nil when there is nothing to report. Keeping the nil case explicit
// avoids the classic "typed nil in an error interface" bug.
func collect(problems []*FieldError) error {
	out := make(ValidationErrors, 0, len(problems))
	for _, p := range problems {
		if p != nil {
			out = append(out, *p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
