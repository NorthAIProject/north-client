// Package spend is the AI cost ledger: one record per model call, and the
// aggregates that answer what a user costs.
//
// It exists because messages.usage cannot answer that question. Most model
// calls are not messages — reports, briefings, memory extraction,
// summarisation, form analysis, workout planning and every embedding go through
// a provider and produce no chat row — so a figure derived from messages
// understates reality by whatever the scheduled sweeps consume. That is the
// larger half for a user who talks to the coach rarely and still gets a weekly
// review.
//
// Nothing here decides a price. internal/ai/pricing owns that; this records
// what pricing computed, so a later correction to a rate does not silently
// rewrite what an account was measured to have cost at the time.
package spend

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Surfaces are the parts of the product that spend. Free text in the column and
// constants here, the same choice quota.Action makes: labelling a new call path
// should be a line of Go rather than a migration, because a label that needs a
// migration is a label that does not get added.
const (
	SurfaceCoach         = "coach"
	SurfaceTelegram      = "telegram"
	SurfaceMCP           = "mcp"
	SurfaceTitle         = "conversation_title"
	SurfaceSummary       = "conversation_summary"
	SurfaceMemory        = "memory_extraction"
	SurfaceWeeklyReview  = "weekly_review"
	SurfaceDailyBriefing = "daily_briefing"
	SurfaceFormAnalysis  = "form_analysis"
	SurfaceWorkoutPlan   = "workout_plan"
	SurfaceEmbedding     = "embedding"

	// SurfaceUnknown labels a call that reached a provider without anyone
	// saying what it was for. Recorded rather than guessed: an unlabelled call
	// is a wiring gap, and it should be visible as one in the spend report
	// instead of being quietly attributed to whichever surface seems likely.
	SurfaceUnknown = "unknown"
)

// Generation is one model call, priced.
type Generation struct {
	// UserID is nil when the call belongs to no account. Recording that as
	// nobody's spend is more honest than attributing it to someone.
	UserID *uuid.UUID

	Surface  string
	Provider string

	// Model is what the provider answered with, not what was configured. Empty
	// when the provider reported nothing, which must stay visible as a gap
	// rather than be guessed into a price.
	Model string

	InputTokens  int
	OutputTokens int

	// CostMicros is micros of the accounting currency. An integer, because
	// money in floating point is how rounding errors become invoices. Zero when
	// pricing had no rate for the model — a gap to fix, not a free call.
	CostMicros int64

	// BYOK marks a call the user's own key paid for. Their spend is not our
	// cost, and counting it would overstate COGS for precisely the users who
	// cost us least.
	BYOK bool
}

// Range is a half-open window: from inclusive, to exclusive.
type Range struct {
	From time.Time
	To   time.Time
}

// UserSpend totals one account over a window.
type UserSpend struct {
	UserID       *uuid.UUID
	Generations  int64
	InputTokens  int64
	OutputTokens int64
	CostMicros   int64
}

// ModelSpend totals one provider and model over a window.
type ModelSpend struct {
	Provider     string
	Model        string
	Generations  int64
	InputTokens  int64
	OutputTokens int64
	CostMicros   int64
}

// SurfaceSpend totals one surface over a window.
type SurfaceSpend struct {
	Surface      string
	Generations  int64
	InputTokens  int64
	OutputTokens int64
	CostMicros   int64
}

// Recorder appends to the ledger.
//
// An interface so the metering decorator in internal/ai can depend on it
// without internal/ai learning about a database, and so a test can assert what
// would have been recorded without one.
type Recorder interface {
	Record(ctx context.Context, g Generation)
}

// Euros renders micros as a decimal string for a report. Integer arithmetic
// throughout: the point of storing micros is not to reintroduce a float on the
// way out.
func Euros(micros int64) string {
	neg := micros < 0
	if neg {
		micros = -micros
	}
	whole := micros / 1_000_000
	frac := micros % 1_000_000
	// Two decimal places, rounded half up, so a report reads like money.
	cents := (frac + 5_000) / 10_000
	if cents == 100 {
		whole++
		cents = 0
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return sign + itoa(whole) + "." + pad2(cents)
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func pad2(v int64) string {
	if v < 10 {
		return "0" + itoa(v)
	}
	return itoa(v)
}
