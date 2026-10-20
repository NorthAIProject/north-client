package moderation

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var (
	// ErrTermsRequired means the person has not accepted the current terms
	// and may not post until they do.
	ErrTermsRequired = errors.New("terms not accepted")
	// ErrSuspended means the person may read but not post.
	ErrSuspended = errors.New("suspended")
)

// TermsVersion is the version of the community terms a person accepts
// before their first post. Raising it asks everybody again.
const TermsVersion = 1

// Reasons somebody can give for a report.
var reasons = map[string]bool{
	"spam": true, "harassment": true, "hate": true, "sexual": true,
	"violence": true, "self_harm": true, "other": true,
}

// Subject is a reportable thing: a crew message, a comment, a person. Kind
// names the slice that owns it, which registers a Hider for that kind.
type Subject struct {
	Kind     string
	ID       uuid.UUID
	AuthorID uuid.UUID
	// ScopeID is where the subject lives, a crew for example, so the people
	// who run it can see its reports. Nil when there is no such place.
	ScopeID *uuid.UUID
}

// Hider is a slice's side of moderation: hiding one of its things once
// enough people have reported it, and how many reports that takes there.
type Hider interface {
	// Hide must be idempotent: hiding something already hidden is a no-op.
	Hide(ctx context.Context, subjectID uuid.UUID) error
	Threshold(ctx context.Context, subjectID uuid.UUID) (int, error)
}
