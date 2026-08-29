// Package memory holds the shape of a durable profile fact.
package memory

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"

	SourceUser       = "user"
	SourceExtraction = "extraction"

	CategoryPreference = "preference"
	CategoryConstraint = "constraint"
	CategoryHabit      = "habit"
	CategoryInjury     = "injury"
	CategoryEquipment  = "equipment"
	CategorySchedule   = "schedule"
	CategoryCoaching   = "coaching"
	CategoryGeneral    = "general"
)

// Categories the product understands. Unknown values are rejected on write.
var Categories = []string{
	CategoryPreference,
	CategoryConstraint,
	CategoryHabit,
	CategoryInjury,
	CategoryEquipment,
	CategorySchedule,
	CategoryCoaching,
	CategoryGeneral,
}

// Statuses a memory may hold.
var Statuses = []string{StatusPending, StatusApproved, StatusRejected}

// Memory is one durable fact about a person.
type Memory struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	Category string
	Content  string
	Status   string
	Pinned   bool
	Excluded bool
	Source   string

	SourceConversationID *uuid.UUID
	Confidence           *float64

	// ValidTo is when this fact stopped being true. Nil means it still is.
	//
	// A retired fact is history, not rubbish: the review page still shows it and
	// nothing deletes it, because "they used to train five days a week" is worth
	// knowing. It simply stops reaching the coach.
	ValidTo *time.Time

	// SupersedesID is the fact this one replaces. Set by extraction as a
	// proposal and acted on when this memory is approved, never before — a
	// rejected extraction must not have retired something true.
	SupersedesID *uuid.UUID

	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsCurrent reports whether this fact is still true.
func (m Memory) IsCurrent() bool { return m.ValidTo == nil }

// IsSuperseded reports whether a newer fact replaced this one.
func (m Memory) IsSuperseded() bool { return m.ValidTo != nil }

// ProposesSupersession reports whether approving this memory would retire
// another one.
func (m Memory) ProposesSupersession() bool { return m.SupersedesID != nil }

// Summary is the coach-facing bullet.
func (m Memory) Summary() string {
	cat := strings.TrimSpace(m.Category)
	if cat == "" {
		cat = CategoryGeneral
	}
	content := strings.TrimSpace(m.Content)
	if m.Pinned {
		return fmt.Sprintf("[%s, pinned] %s", cat, content)
	}
	return fmt.Sprintf("[%s] %s", cat, content)
}

// IsApproved reports whether the coach may see this memory.
func (m Memory) IsApproved() bool { return m.Status == StatusApproved }
