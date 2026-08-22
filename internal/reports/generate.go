package reports

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/prompts"
	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
)

// briefingMaxTokens caps the morning briefing. The prompt asks for three to
// five sentences; this is the backstop for when the model ignores that, so a
// runaway generation cannot turn the cheap path into the expensive one.
const briefingMaxTokens = 400

// Generator is the non-streaming AI call a review needs.
type Generator interface {
	Generate(ctx context.Context, req ai.Request) (*ai.Response, error)
}

type chainGenerator struct {
	runner *ai.Runner
}

// ClientFromChain walks the provider chain until one of them answers.
//
// A weekly review is generated once and read for a week. Failing it because the
// head provider happened to be overloaded that minute is the kind of outage
// nobody notices until the review is missing, so it falls through like the
// coach does. Reviews run in the worker with no user in hand, so the tier is
// empty and the default chain serves.
func ClientFromChain(runner *ai.Runner) Generator {
	return chainGenerator{runner: runner}
}

func (g chainGenerator) Generate(ctx context.Context, req ai.Request) (*ai.Response, error) {
	var resp *ai.Response

	_, err := g.runner.Run(ctx, ai.RunOptions{}, func(c ai.Client) error {
		r, err := c.Generate(ctx, req)
		resp = r
		return err
	})
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// ReviewContext is the week, in the person's own recorded words.
type ReviewContext struct {
	Goals     []string
	CheckIns  []string
	Training  []string
	Nutrition []string
	Sleep     []string
	Hydration []string
	Habits    []string
	Memories  []string
}

// ContextLoader collects the recorded week. Tests inject a stub; production
// wires the same slices the coach already reads.
type ContextLoader interface {
	Load(ctx context.Context, user users.User, start, end time.Time) (ReviewContext, error)
}

// Generate writes the review body. Failures mark the row failed and return
// the error, so the worker can retry without losing the slot.
func (s *Service) Generate(ctx context.Context, id, userID uuid.UUID) error {
	report, err := s.repo.Get(ctx, id, userID)
	if err != nil {
		return err
	}

	user, err := s.users.ByID(ctx, userID)
	if err != nil {
		return s.fail(ctx, id, userID, err)
	}

	var review ReviewContext
	if s.context != nil {
		review, err = s.context.Load(ctx, user, report.PeriodStart, report.PeriodEnd)
		if err != nil {
			return s.fail(ctx, id, userID, err)
		}
	}

	if s.client == nil {
		return s.fail(ctx, id, userID, apperr.New("report generation is not configured"))
	}

	name := prompts.WeeklyReview
	if report.Kind == KindDaily {
		name = prompts.DailyBriefing
	}
	prompt, err := prompts.Render(name, map[string]string{
		"Title":   report.Title,
		"Context": formatContext(user, report, review),
	})
	if err != nil {
		return s.fail(ctx, id, userID, err)
	}

	// A briefing is read once and thrown away, so it runs on the cheap model and
	// is capped short. The weekly review keeps the default model: it is the
	// artefact somebody re-reads in six months.
	req := ai.Request{Messages: []ai.Message{ai.UserText(prompt)}}
	if report.Kind == KindDaily {
		req.Model = s.fastModel
		req.MaxTokens = briefingMaxTokens
	}

	// The single largest unattributed cost before the ledger existed: a weekly
	// review reads a whole week of context and runs for every active account,
	// on a schedule nobody asked for individually.
	surface := spend.SurfaceWeeklyReview
	if report.Kind == KindDaily {
		surface = spend.SurfaceDailyBriefing
	}

	resp, err := s.client.Generate(aiattr.WithUser(ctx, userID, surface), req)
	if err != nil {
		return s.fail(ctx, id, userID, err)
	}

	body := strings.TrimSpace(resp.Text)
	if body == "" {
		return s.fail(ctx, id, userID, apperr.New("the model returned an empty review"))
	}

	_, err = s.repo.SaveGenerated(ctx, id, userID, body)
	if err != nil {
		return err
	}

	if report.Kind == KindDaily {
		if s.notify != nil {
			if notifyErr := s.notify.Notify(ctx, userID, body); notifyErr != nil {
				// The briefing is stored. A failed push must not mark it failed
				// or the next sweep would refuse to rewrite it.
				slog.Default().Warn("reports: could not send briefing", "error", notifyErr, "user_id", userID)
			}
		}
		if s.inbox != nil {
			preview := body
			if len(preview) > 140 {
				preview = strings.TrimSpace(preview[:140]) + "…"
			}
			_ = s.inbox.Note(ctx, userID, "briefing_ready", report.ID.String(),
				"Today's briefing", preview, "/app/reports/"+report.ID.String())
		}
	}
	return nil
}

func (s *Service) fail(ctx context.Context, id, userID uuid.UUID, cause error) error {
	_, _ = s.repo.MarkFailed(ctx, id, userID, userFacing(cause))
	return cause
}

func userFacing(err error) string {
	switch {
	case apperr.Is(err, apperr.ErrValidation), apperr.Is(err, apperr.ErrNotFound), apperr.Is(err, apperr.ErrConflict):
		return err.Error()
	default:
		return "Generation failed. Try again in a minute."
	}
}

func formatContext(user users.User, report Report, review ReviewContext) string {
	var b strings.Builder
	b.WriteString("Person: ")
	b.WriteString(user.DisplayName)
	b.WriteString("\nTimezone: ")
	b.WriteString(user.Timezone)
	if report.Kind == KindDaily {
		b.WriteString("\nDay: ")
		b.WriteString(report.PeriodStart.Format("Monday 2 Jan 2006"))
	} else {
		b.WriteString("\nWeek: ")
		b.WriteString(report.PeriodStart.Format("2 Jan 2006"))
		b.WriteString(" – ")
		b.WriteString(report.PeriodEnd.Add(-time.Second).Format("2 Jan 2006"))
	}
	b.WriteString("\n")

	writeSection(&b, "Goals", review.Goals)
	writeSection(&b, "Check-ins", review.CheckIns)
	writeSection(&b, "Training", review.Training)
	writeSection(&b, "Nutrition", review.Nutrition)
	writeSection(&b, "Sleep", review.Sleep)
	writeSection(&b, "Hydration", review.Hydration)
	writeSection(&b, "Habits", review.Habits)
	writeSection(&b, "Remembered facts", review.Memories)
	return strings.TrimSpace(b.String())
}

func writeSection(b *strings.Builder, title string, lines []string) {
	b.WriteString("\n")
	b.WriteString(title)
	b.WriteString(":\n")
	if len(lines) == 0 {
		b.WriteString("(none recorded)\n")
		return
	}
	for _, line := range lines {
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
	}
}
