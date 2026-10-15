package xp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	lbpages "github.com/NorthAIProject/north-client/web/leaderboard"
)

// Handler is the leaderboard page. Mount under /app.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/friends/leaderboard", h.page)
}

// boards maps a ?board= key to a metric and period; an unknown key is the
// first board rather than an error.
var boards = map[string][2]string{
	"xp-week":  {MetricXP, PeriodWeek},
	"xp-all":   {MetricXP, PeriodAll},
	"streak":   {MetricStreak, ""},
	"workouts": {MetricWorkouts, ""},
}

// kindLabel says what a kind pays for, rules included, in the request's
// language. A kind without a label reads as its code.
func kindLabel(ctx context.Context, kind string) string {
	switch kind {
	case KindWorkout:
		return i18n.Tf(ctx, "xp.kind.workout", WorkoutMinMinutes, WorkoutsPaidPerDay)
	case KindStreakDay:
		return i18n.Tf(ctx, "xp.kind.streak_day", StreakDayFrom)
	case KindStreakMark:
		return i18n.Tf(ctx, "xp.kind.streak_mark", markList(ctx))
	case KindHabitKept, KindMilestone, KindGoal, KindWeekReviewed, KindChallengeMet:
		return i18n.T(ctx, "xp.kind."+kind)
	}
	return kind
}

// markList is the streak marks as a sentence: "7, 30, 100 or 365".
func markList(ctx context.Context) string {
	marks := make([]string, 0, len(achievements.StreakMarks))
	for _, m := range achievements.StreakMarks {
		marks = append(marks, strconv.Itoa(m))
	}
	last := len(marks) - 1
	return i18n.Tf(ctx, "xp.marks.or", strings.Join(marks[:last], ", "), marks[last])
}

// switchLabel names the sharing switch a metric needs, as the Friends page
// words it.
func switchLabel(ctx context.Context, metric string) string {
	switch metric {
	case MetricXP, MetricStreak, MetricWorkouts:
		return i18n.T(ctx, "xp.switch."+metric)
	}
	return ""
}

func (h *Handler) page(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	now := time.Now()
	key := r.URL.Query().Get("board")
	choice, ok := boards[key]
	if !ok {
		key, choice = "xp-week", boards["xp-week"]
	}

	summary, err := h.svc.Summary(r.Context(), user, now)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	board, err := h.svc.Board(r.Context(), user, choice[0], choice[1], now)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	p := lbpages.Page{
		Level: lbpages.Level{
			Number: summary.Level.Number, Title: summary.Level.Title, Total: summary.Total,
			Floor: summary.Level.Floor, Next: summary.Level.Next,
		},
		WeekTotal: summary.WeekTotal, Board: key, Sharing: board.Sharing, ShareSwitch: switchLabel(r.Context(), board.Metric),
	}
	for _, e := range summary.Week {
		p.Week = append(p.Week, lbpages.Kind{Label: kindLabel(r.Context(), e.Kind), Count: e.Count, Points: e.Points})
	}
	for _, e := range board.Entries {
		row := lbpages.Row{Rank: e.Rank, Name: e.DisplayName, Handle: e.Handle, Value: valueText(r.Context(), board.Metric, e.Value), Me: e.Me}
		if e.Level != nil {
			row.Level = e.Level.Title
		}
		p.Rows = append(p.Rows, row)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := lbpages.LeaderboardPage(user, p).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}

func valueText(ctx context.Context, metric string, v int) string {
	switch metric {
	case MetricStreak:
		if v == 1 {
			return i18n.T(ctx, "xp.value.day.one")
		}
		return i18n.Tf(ctx, "xp.value.day.many", v)
	case MetricWorkouts:
		if v == 1 {
			return i18n.T(ctx, "xp.value.workout.one")
		}
		return i18n.Tf(ctx, "xp.value.workout.many", v)
	}
	// XP is the unit's name in every language North speaks.
	return fmt.Sprintf("%d XP", v)
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	middleware.FromContext(r.Context()).Error("leaderboard request failed", slog.Any("error", err))
	http.Error(w, i18n.T(r.Context(), "xp.err.failed"), http.StatusInternalServerError)
}
