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
	Source   string

	SourceConversationID *uuid.UUID
	Confidence           *float64

	CreatedAt time.Time
	UpdatedAt time.Time
}

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
