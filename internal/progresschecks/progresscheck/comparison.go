package progresscheck

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Comparison is a model's observation of what changed between a person's
// first progress check with photos and their latest. An observation, never a
// measurement: it is shown and handed to the coach labelled as one.
type Comparison struct {
	FromCheckID, ToCheckID uuid.UUID
	FromOn, ToOn           time.Time

	// Summary is a few neutral sentences on visible change.
	Summary string
	// Confidence is low, medium or high.
	Confidence string
	// Limits is what made the photos hard to compare: pose, light, clothing.
	Limits string
	// Provider is who read the photos, as the person was told before sending.
	Provider string

	CreatedAt time.Time
}

// Bounds on what a model writes, under the table's CHECKs so a clean
// comparison always stores.
const (
	maxComparisonSummary = 600
	maxComparisonLimits  = 280
)

// Clean makes a model's reply safe to store and show. Whitespace is
// collapsed, the text is cut to fit, and an unknown confidence reads as low.
// An empty summary is not a comparison; Clean reports it with ok false.
func (c Comparison) Clean() (Comparison, bool) {
	c.Summary = clip(c.Summary, maxComparisonSummary)
	c.Limits = clip(c.Limits, maxComparisonLimits)
	c.Confidence = strings.ToLower(strings.TrimSpace(c.Confidence))
	if !validConfidence(c.Confidence) {
		c.Confidence = "low"
	}
	return c, c.Summary != ""
}

// Stale reports a comparison whose latest check is no longer the newest one
// with photos, so comparing again would cover more.
func (c Comparison) Stale(newestWithPhotos uuid.UUID) bool {
	return c.ToCheckID != newestWithPhotos
}

func clip(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		s = strings.TrimSpace(string(r[:max-1])) + "…"
	}
	return s
}

// CoachLine is the comparison as the coach reads it: dated, and labelled as a
// model's observation from photos so it is never taken for a measurement.
func (c Comparison) CoachLine() string {
	line := fmt.Sprintf("Photo comparison of the first progress check (%s) with the latest (%s), a model's observation from photos with %s confidence, not a measurement: %s",
		c.FromOn.Format("2 Jan 2006"), c.ToOn.Format("2 Jan 2006"), c.Confidence, c.Summary)
	if c.Limits != "" {
		line += " Limits: " + c.Limits
	}
	return line
}
