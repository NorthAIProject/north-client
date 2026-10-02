// Package item holds the capture inbox's types.
package item

import (
	"time"

	"github.com/google/uuid"
)

// Sources an item can come from.
const (
	SourceApp      = "app"
	SourceShare    = "share"
	SourceShortcut = "shortcut"
	SourceWeb      = "web"
)

// Statuses.
const (
	StatusOpen      = "open"
	StatusFiled     = "filed"
	StatusDismissed = "dismissed"
)

// Destinations an item can be filed to.
const (
	DestinationGoalNote  = "goal_note"
	DestinationKnowledge = "knowledge"
	DestinationJournal   = "journal"
)

// MaxText bounds one item: a paragraph or a pasted link with a note, not a
// document. Documents go to Knowledge.
const MaxText = 4000

// ValidSource reports whether s is a known source.
func ValidSource(s string) bool {
	return s == SourceApp || s == SourceShare || s == SourceShortcut || s == SourceWeb
}

// ValidDestination reports whether d is a known destination.
func ValidDestination(d string) bool {
	return d == DestinationGoalNote || d == DestinationKnowledge || d == DestinationJournal
}

// Suggestion is where the coach thinks an item belongs, and why. GoalID is
// set for a goal note; Title for a knowledge note.
type Suggestion struct {
	Destination string     `json:"destination"`
	GoalID      *uuid.UUID `json:"goal_id,omitempty"`
	GoalTitle   string     `json:"goal_title,omitempty"`
	Title       string     `json:"title,omitempty"`
	Why         string     `json:"why"`
}

// Item is one thing saved to the inbox.
type Item struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Source     string
	Text       string
	Suggestion *Suggestion
	Status     string
	CreatedAt  time.Time
}

// Filing is where the person chose to put an item.
type Filing struct {
	Destination string
	// GoalID is required for a goal note.
	GoalID uuid.UUID
	// Title names a knowledge note; empty uses the start of the text.
	Title string
}
