// Package report holds the shape of a stored weekly review.
//
// Leaf package so templates and handlers can import it without cycles.
package report

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Kind string

const (
	KindWeekly Kind = "weekly"
	// KindDaily is the short morning briefing. It shares this table with the
	// weekly review: the reports schema already keys its one-active-row index on
	// (user_id, kind, period_start), so a daily row needs no migration, only a
	// period whose start and end are the same day.
	KindDaily Kind = "daily"
)

type Status string

const (
	StatusPending Status = "pending"
	StatusReady   Status = "ready"
	StatusFailed  Status = "failed"
)

// Report is one stored review. Body is markdown the person reads.
type Report struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	Kind        Kind
	PeriodStart time.Time
	PeriodEnd   time.Time
	Title       string
	Body        string
	Status      Status
	LastError   string
	GeneratedAt *time.Time
	ArchivedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (r Report) Archived() bool { return r.ArchivedAt != nil }

func (r Report) Ready() bool { return r.Status == StatusReady && r.Body != "" }

func (r Report) Summary() string {
	return fmt.Sprintf("%s (%s)", r.Title, r.Status)
}
