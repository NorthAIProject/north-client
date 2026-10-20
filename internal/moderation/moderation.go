package moderation

import (
	"context"
	"errors"
	"time"

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
// enough people have reported it, how many reports that takes there, and
// what an operator reviewing it reads.
type Hider interface {
	// Hide and Unhide must be idempotent.
	Hide(ctx context.Context, subjectID uuid.UUID) error
	Unhide(ctx context.Context, subjectID uuid.UUID) error
	Threshold(ctx context.Context, subjectID uuid.UUID) (int, error)
	// Excerpt is the reported words, short, for the operator queue; "" when
	// the subject is gone.
	Excerpt(ctx context.Context, subjectID uuid.UUID) (string, error)
}

// ThreadSender delivers a push to every device a person has. The APNs and
// web-push services are both one.
type ThreadSender interface {
	SendThread(ctx context.Context, userID uuid.UUID, title, body, href, category, threadID string) (int, error)
}

// ReportGroup is one reported subject in the operator queue: every open
// report on it together.
type ReportGroup struct {
	Kind      string
	SubjectID uuid.UUID
	AuthorID  *uuid.UUID
	Reasons   map[string]int
	Count     int
	Hidden    bool
	Excerpt   string
	FirstAt   time.Time
}

// Operator decisions on a reported subject.
const (
	DecideHide    = "hide"
	DecideUnhide  = "unhide"
	DecideDismiss = "dismiss"
)
