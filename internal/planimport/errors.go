// Package planimport reads a workout plan or a meal plan out of a file someone
// already has — a coach's spreadsheet, a PDF, a photo of a printout — and turns
// it into a draft they can correct before anything is saved.
//
// It writes nothing. Like capture, the split is the point: reading a file can
// be wrong, so a parse only ever produces a draft, and the plan is created by a
// separate call made after the person has seen and fixed it. Cancelling is
// simply not making that call.
//
// Two rules run through the whole package:
//
//   - Never invent. A blank cell stays blank. A missing set count is "not
//     stated", not 3. A food with no gram weight is flagged, not estimated.
//   - Never touch the macro target. A meal import is measured against the
//     person's active target; it never recalculates or replaces it.
package planimport

import (
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Reason classifies why a file was refused, so tests and callers can tell the
// cases apart without comparing sentences.
type Reason string

const (
	ReasonEmpty             Reason = "empty"
	ReasonTooLarge          Reason = "too_large"
	ReasonTooLong           Reason = "too_long"
	ReasonUnsupported       Reason = "unsupported"
	ReasonPasswordProtected Reason = "password_protected"
	ReasonUnreadable        Reason = "unreadable"
	ReasonNotAPlan          Reason = "not_a_plan"
	ReasonCannotRead        Reason = "cannot_read"
	ReasonBusy              Reason = "busy"
)

// FileError is a file the import refused, with a sentence the person can act
// on.
//
// It unwraps to a FieldErrors on "file", so both the web form and the JSON API
// already know how to show it: next to the upload control, as a 422. Nothing
// here is a server fault, and none of it should read like one.
type FileError struct {
	Reason  Reason
	Message string
}

func (e *FileError) Error() string { return e.Message }

func (e *FileError) Unwrap() error {
	if e.Reason == ReasonBusy {
		return apperr.Wrap(apperr.ErrConflict, "%s", e.Message)
	}
	return apperr.FieldErrors{{Field: "file", Message: e.Message}}
}

func refuse(reason Reason, message string) error {
	return &FileError{Reason: reason, Message: message}
}

// ReasonOf reports why err refused a file, or "" when it is not a FileError.
func ReasonOf(err error) Reason {
	var fe *FileError
	if apperr.As(err, &fe) {
		return fe.Reason
	}
	return ""
}
