// Package analysis holds the shape of a form analysis.
//
// A leaf, for the same reason internal/workouts/plan is one: the media service
// and the templates that render an analysis both import it, so the handler can
// import its templates without a cycle.
package analysis

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
)

// FormAnalysis is what the coach saw in a video.
type FormAnalysis struct {
	Exercise string `json:"exercise"`

	// Confidence is how clearly the footage shows the movement. Low confidence
	// with no issues is a valid and useful result: it means the camera angle
	// cannot support a judgement, which is the honest answer far more often
	// than people expect.
	Confidence string `json:"confidence"`

	Summary string      `json:"summary"`
	Issues  []FormIssue `json:"issues"`
}

// FormIssue is one observation, anchored to the moment it is visible.
type FormIssue struct {
	// TimestampSeconds is what makes the analysis checkable: the person jumps
	// to that moment and sees it for themselves rather than taking the model's
	// word for it.
	TimestampSeconds float64 `json:"timestamp_seconds"`

	Severity    string `json:"severity"`
	Observation string `json:"observation"`
	Correction  string `json:"correction"`
}

const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"

	SeverityCritical = "critical"
	SeverityModerate = "moderate"
	SeverityMinor    = "minor"
)

// Timestamp renders the moment as m:ss for display.
func (i FormIssue) Timestamp() string {
	total := int(i.TimestampSeconds)
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

// Trustworthy reports whether the model saw enough to be worth acting on.
func (a FormAnalysis) Trustworthy() bool {
	return a.Confidence == ConfidenceHigh || a.Confidence == ConfidenceMedium
}

// Clean reports a set that produced no findings from footage the model could
// actually read. Worth saying out loud: a clean set is a result, not a failure
// to find something.
func (a FormAnalysis) Clean() bool {
	return a.Trustworthy() && len(a.Issues) == 0
}

// Schema is the shape the model must return.
//
// Ordering matters: the model names the exercise and states its confidence
// before listing issues, so it commits to whether it can see the movement
// before it starts finding faults in it.
func Schema() *ai.Schema {
	issue := ai.Object("one fault visible in the video", map[string]*ai.Schema{
		"timestamp_seconds": ai.Number("the moment this is visible, in seconds from the start"),
		"severity":          ai.Enum("how serious this is", SeverityCritical, SeverityModerate, SeverityMinor),
		"observation":       ai.String("what is visibly happening, stated plainly"),
		"correction":        ai.String("one actionable cue to fix it"),
	}, "timestamp_seconds", "severity", "observation", "correction")

	return ai.Object("an assessment of exercise form", map[string]*ai.Schema{
		"exercise":   ai.String("the exercise being performed, or 'unclear' if it cannot be identified"),
		"confidence": ai.Enum("how clearly the video shows the movement", ConfidenceHigh, ConfidenceMedium, ConfidenceLow),
		"summary":    ai.String("two or three sentences on the lift overall and the single most important thing to work on"),
		"issues":     ai.Array("faults actually visible in this video; empty when confidence is low or the lift is clean", issue),
	}, "exercise", "confidence", "summary", "issues")
}

// Sanitise enforces the rules the prompt asks for, in case the model does not.
//
// A model that reports faults while admitting it cannot see the movement is
// guessing, and guessing is the failure mode this whole feature has to avoid:
// it sends someone away to fix a problem they do not have. Dropping those
// findings costs nothing when they were real and prevents real harm when they
// were not.
func Sanitise(a FormAnalysis) FormAnalysis {
	a.Confidence = strings.ToLower(strings.TrimSpace(a.Confidence))
	switch a.Confidence {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
	default:
		a.Confidence = ConfidenceLow
	}

	if a.Confidence == ConfidenceLow {
		a.Issues = nil
		return a
	}

	kept := make([]FormIssue, 0, len(a.Issues))
	for _, issue := range a.Issues {
		issue.Severity = strings.ToLower(strings.TrimSpace(issue.Severity))
		switch issue.Severity {
		case SeverityCritical, SeverityModerate, SeverityMinor:
		default:
			issue.Severity = SeverityMinor
		}

		// An observation with no timestamp cannot be checked against the video,
		// which is the only thing that makes it verifiable.
		if strings.TrimSpace(issue.Observation) == "" {
			continue
		}
		if issue.TimestampSeconds < 0 {
			issue.TimestampSeconds = 0
		}

		kept = append(kept, issue)
	}
	a.Issues = kept

	return a
}

// Status values for an analysis.
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

// Analysis is a form analysis and its progress.
//
// It lives here rather than in the media package so the templates that render
// it can be imported by the handler without the two importing each other.
type Analysis struct {
	ID      uuid.UUID
	MediaID uuid.UUID
	UserID  uuid.UUID
	Status  string

	// Result is present only once Status is done.
	Result *FormAnalysis

	Error    string
	Model    string
	Provider string

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (a Analysis) IsDone() bool   { return a.Status == StatusDone }
func (a Analysis) IsFailed() bool { return a.Status == StatusFailed }
func (a Analysis) InProgress() bool {
	return a.Status == StatusPending || a.Status == StatusRunning
}
