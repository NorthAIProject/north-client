// Package journal holds the shape of a free-form reflection entry.
//
// A leaf, so the mind service and any future template that renders one do
// not import each other. See CLAUDE.md on slice layout.
package journal

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Entry is one free-form reflection, optionally rated 1-5 like a check-in's
// mood.
type Entry struct {
	ID     uuid.UUID
	UserID uuid.UUID

	Content string
	Mood    *int

	CreatedAt time.Time
}

// Summary renders an entry for the coach's context.
func (e Entry) Summary() string {
	text := truncate(e.Content, 200)
	if e.Mood != nil {
		return fmt.Sprintf("%s (mood %d/5): %s", e.CreatedAt.Format("2 Jan"), *e.Mood, text)
	}
	return fmt.Sprintf("%s: %s", e.CreatedAt.Format("2 Jan"), text)
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
