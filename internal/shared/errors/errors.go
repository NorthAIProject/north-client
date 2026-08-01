// Package errors provides the small set of sentinel errors that services return
// and handlers translate into HTTP responses.
//
// Services must not know about HTTP status codes, and handlers must not repeat
// the same string comparisons in every route. A service returns one of the
// sentinels below (wrapped with context), and the presentation layer maps it to
// a status in exactly one place.
package errors

import (
	"errors"
	"fmt"
)

// Re-exported so callers need only one errors import.
var (
	Is     = errors.Is
	As     = errors.As
	New    = errors.New
	Join   = errors.Join
	Unwrap = errors.Unwrap
)

var (
	// ErrNotFound means the requested record does not exist, or exists but is
	// not visible to this user. The two cases are deliberately indistinguishable
	// so that a 404 never confirms the existence of another user's data.
	ErrNotFound = errors.New("not found")

	// ErrConflict means the request collides with existing state, such as
	// signing up with an email that is already registered.
	ErrConflict = errors.New("conflict")

	// ErrValidation means the input is malformed or fails a business rule.
	ErrValidation = errors.New("validation failed")

	// ErrUnauthenticated means no valid session was presented.
	ErrUnauthenticated = errors.New("unauthenticated")

	// ErrForbidden means the caller is known but not allowed to do this.
	ErrForbidden = errors.New("forbidden")

	// ErrUnavailable means a dependency (AI provider, storage) failed in a way
	// that may succeed on retry.
	ErrUnavailable = errors.New("temporarily unavailable")
)

// Wrap adds context to an error while preserving the sentinel underneath, so
// that errors.Is still matches after the error has crossed several layers.
// It returns nil when err is nil, which lets callers wrap unconditionally.
func Wrap(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}

// FieldError describes a single invalid input field. Handlers render these next
// to the offending form control rather than as a page-level error.
type FieldError struct {
	Field   string
	Message string
}

func (e FieldError) Error() string { return e.Field + ": " + e.Message }

// Unwrap reports FieldError as a validation failure, so a caller that only
// cares about the category can match ErrValidation without type assertions.
func (e FieldError) Unwrap() error { return ErrValidation }

// FieldErrors is a set of per-field validation failures.
type FieldErrors []FieldError

func (e FieldErrors) Error() string {
	if len(e) == 0 {
		return "validation failed"
	}
	msg := e[0].Error()
	if len(e) > 1 {
		msg = fmt.Sprintf("%s (and %d more)", msg, len(e)-1)
	}
	return msg
}

func (e FieldErrors) Unwrap() error { return ErrValidation }

// Messages returns the failures keyed by field name, ready for a template.
func (e FieldErrors) Messages() map[string]string {
	if len(e) == 0 {
		return nil
	}
	out := make(map[string]string, len(e))
	for _, fe := range e {
		// First failure per field wins: it is usually the most fundamental one
		// ("email is required" is more useful than "email is malformed").
		if _, seen := out[fe.Field]; !seen {
			out[fe.Field] = fe.Message
		}
	}
	return out
}

// Add appends a field failure and returns the set, so validation reads as a
// straight sequence of checks.
func (e FieldErrors) Add(field, message string) FieldErrors {
	return append(e, FieldError{Field: field, Message: message})
}

// OrNil returns nil when there are no failures, so callers can write
// `return errs.OrNil()` without a length check.
func (e FieldErrors) OrNil() error {
	if len(e) == 0 {
		return nil
	}
	return e
}
