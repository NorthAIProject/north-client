// Package decision holds the shape of a recorded call.
//
// A leaf, so the decisions service and the templates that render one do not
// import each other. See CLAUDE.md on slice layout.
package decision

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Decision is one point-in-time call: what was chosen, what else was on the
// table, why, and (once looked back on) what happened.
type Decision struct {
	ID     uuid.UUID
	UserID uuid.UUID

	Title     string
	Options   string
	Rationale string
	Outcome   string

	// Held is whether the call held up when looked back on: yes, partly or
	// no; empty until answered. HeldAt is when it was last answered.
	Held   string
	HeldAt *time.Time

	DecidedAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Revisit marks are the days after a decision when the coach asks whether it
// held.
var RevisitMarks = []int{30, 90}

// Answers to "did it hold?".
const (
	HeldYes    = "yes"
	HeldPartly = "partly"
	HeldNo     = "no"
)

// Revisit is a decision due to be looked back on, mark days after it was made.
type Revisit struct {
	ID    uuid.UUID
	Title string
	Mark  int
}

// Calibration counts how calls held up among those looked back on.
type Calibration struct {
	Yes, Partly, No int
}

// Revisited is how many calls have an answer.
func (c Calibration) Revisited() int { return c.Yes + c.Partly + c.No }

// Summary is the coach-facing one-liner. Empty optional fields are omitted
// so a freshly logged call is not padded with "Outcome: —".
func (d Decision) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s", d.DecidedAt.Format("2 Jan"), d.Title)
	if opts := strings.TrimSpace(d.Options); opts != "" {
		fmt.Fprintf(&b, ". Options: %s", truncate(opts, 120))
	}
	if why := strings.TrimSpace(d.Rationale); why != "" {
		fmt.Fprintf(&b, ". Why: %s", truncate(why, 120))
	}
	if out := strings.TrimSpace(d.Outcome); out != "" {
		fmt.Fprintf(&b, ". Outcome: %s", truncate(out, 120))
	}
	if d.Held != "" {
		fmt.Fprintf(&b, ". Held: %s", d.Held)
	}
	return b.String()
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
