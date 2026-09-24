package dashboard

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/goals/goal"
	"github.com/NorthAIProject/north-client/internal/nudges/nudge"
	"github.com/NorthAIProject/north-client/internal/reports/report"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

type SessionResolver interface {
	Resolve(context.Context, string) (auth.Session, error)
}

type API struct {
	svc      *Service
	sessions SessionResolver
}

func NewAPI(svc *Service, sessions SessionResolver) *API {
	return &API{svc: svc, sessions: sessions}
}

func (a *API) Routes(r chi.Router) {
	r.Get("/today", a.today)
}

type TodayResponse struct {
	User     auth.APIUser  `json:"user"`
	Snapshot TodaySnapshot `json:"snapshot"`
}

type TodaySnapshot struct {
	Range            string             `json:"range"`
	CheckedInToday   bool               `json:"checkedInToday"`
	Streak           int                `json:"streak"`
	GoalActivity7d   int                `json:"goalActivity7d"`
	PendingMemories  int                `json:"pendingMemories"`
	Goals            []Goal             `json:"goals"`
	LastThread       *Thread            `json:"lastThread,omitempty"`
	NextStep         *NextStepResponse  `json:"nextStep,omitempty"`
	Hydration        HydrationResponse  `json:"hydration"`
	Sleep            SleepResponse      `json:"sleep"`
	ActivityCalories float64            `json:"activityCalories"`
	Timeline         []TimelineResponse `json:"timeline"`
	Deltas           DeltasResponse     `json:"deltas"`
	Nudges           []NudgeResponse    `json:"nudges"`
	Briefing         string             `json:"briefing,omitempty"`
}

type Goal struct {
	ID             uuid.UUID  `json:"id"`
	Title          string     `json:"title"`
	Category       string     `json:"category"`
	Status         string     `json:"status"`
	TargetDate     *time.Time `json:"targetDate,omitempty"`
	MilestoneTotal int        `json:"milestoneTotal"`
	MilestoneDone  int        `json:"milestoneDone"`
	Progress       *int       `json:"progress,omitempty"`
}

type Thread struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Kind      string    `json:"kind"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type NextStepResponse struct {
	Kind    string `json:"kind"`
	Eyebrow string `json:"eyebrow"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	CTA     string `json:"cta"`
	Href    string `json:"href"`
}

type HydrationResponse struct {
	TodayML  int `json:"todayML"`
	TargetML int `json:"targetML"`
	Percent  int `json:"percent"`
}

type SleepResponse struct {
	Logged          bool `json:"logged"`
	DurationMinutes int  `json:"durationMinutes"`
	Quality         *int `json:"quality,omitempty"`
}

type TimelineResponse struct {
	Kind   string    `json:"kind"`
	Label  string    `json:"label"`
	At     time.Time `json:"at"`
	Title  string    `json:"title"`
	Detail string    `json:"detail,omitempty"`
	Href   string    `json:"href,omitempty"`
	Icon   string    `json:"icon"`
}

type DeltaResponse struct {
	Pct       float64 `json:"pct"`
	Direction int     `json:"direction"`
	HasPrior  bool    `json:"hasPrior"`
}

type DeltasResponse struct {
	Hydration  DeltaResponse `json:"hydration"`
	SleepHours DeltaResponse `json:"sleepHours"`
	Calories   DeltaResponse `json:"calories"`
	CheckIns   DeltaResponse `json:"checkIns"`
}

type NudgeResponse struct {
	ID        uuid.UUID `json:"id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Href      string    `json:"href"`
	CreatedAt time.Time `json:"createdAt"`
}

func (a *API) today(w http.ResponseWriter, r *http.Request) {
	user, ok := a.user(w, r)
	if !ok {
		return
	}

	rg := timerange.Parse(r.URL.Query().Get("range"), user.Location())
	snapshot, err := a.svc.Load(r.Context(), user, rg)
	if err != nil {
		httpx.Error(w, err, "Today could not be loaded.")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, TodayResponse{
		User:     auth.ProjectUser(user),
		Snapshot: projectSnapshot(snapshot),
	})
}

func (a *API) user(w http.ResponseWriter, r *http.Request) (users.User, bool) {
	token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	token = strings.TrimSpace(token)
	if !found || token == "" {
		httpx.Error(w, apperr.ErrUnauthenticated, "A bearer token is required.")
		return users.User{}, false
	}

	session, err := a.sessions.Resolve(r.Context(), token)
	if err != nil {
		if apperr.Is(err, apperr.ErrUnauthenticated) || apperr.Is(err, apperr.ErrNotFound) {
			httpx.Error(w, apperr.ErrUnauthenticated, "That token is not valid.")
		} else {
			httpx.Error(w, apperr.ErrUnavailable, "Something went wrong.")
		}
		return users.User{}, false
	}
	return session.User, true
}

func projectSnapshot(snapshot Snapshot) TodaySnapshot {
	step, hasStep := PickNextStep(snapshot)
	var nextStep *NextStepResponse
	if hasStep {
		nextStep = &NextStepResponse{Kind: step.Kind, Eyebrow: step.Eyebrow, Title: step.Title, Body: step.Body, CTA: step.CTA, Href: step.Href}
	}

	return TodaySnapshot{
		Range:            snapshot.Range.Key,
		CheckedInToday:   snapshot.CheckedInToday,
		Streak:           snapshot.Streak,
		GoalActivity7d:   snapshot.GoalActivity7d,
		PendingMemories:  snapshot.PendingMemories,
		Goals:            projectGoals(snapshot.Goals),
		LastThread:       projectThread(snapshot.LastThread),
		NextStep:         nextStep,
		Hydration:        HydrationResponse{TodayML: snapshot.Hydration.TodayML, TargetML: snapshot.Hydration.TargetML, Percent: snapshot.Hydration.Percent},
		Sleep:            SleepResponse{Logged: snapshot.Sleep.Logged, DurationMinutes: snapshot.Sleep.DurationMinutes, Quality: snapshot.Sleep.Quality},
		ActivityCalories: snapshot.ActivityCalories,
		Timeline:         projectTimeline(snapshot.Timeline),
		Deltas:           DeltasResponse{Hydration: projectDelta(snapshot.Deltas.Hydration), SleepHours: projectDelta(snapshot.Deltas.SleepHours), Calories: projectDelta(snapshot.Deltas.Calories), CheckIns: projectDelta(snapshot.Deltas.CheckIns)},
		Nudges:           projectNudges(snapshot.Nudges),
		Briefing:         briefingText(snapshot.Briefing),
	}
}

func projectGoals(goals []goal.Goal) []Goal {
	out := make([]Goal, len(goals))
	for i, item := range goals {
		var targetDate *time.Time
		if !item.TargetDate.IsZero() {
			value := item.TargetDate
			targetDate = &value
		}
		progress, hasProgress := item.Progress()
		var progressPtr *int
		if hasProgress {
			progressPtr = &progress
		}
		out[i] = Goal{ID: item.ID, Title: item.Title, Category: item.Category, Status: item.Status, TargetDate: targetDate, MilestoneTotal: item.MilestoneTotal, MilestoneDone: item.MilestoneDone, Progress: progressPtr}
	}
	return out
}

func projectThread(thread *conversations.Conversation) *Thread {
	if thread == nil {
		return nil
	}
	return &Thread{ID: thread.ID, Title: thread.Title, Kind: thread.Kind, UpdatedAt: thread.UpdatedAt}
}

func projectTimeline(entries []Entry) []TimelineResponse {
	out := make([]TimelineResponse, len(entries))
	for i, entry := range entries {
		out[i] = TimelineResponse{Kind: string(entry.Kind), Label: entry.Kind.Label(), At: entry.At, Title: entry.Title, Detail: entry.Detail, Href: entry.Href, Icon: entry.Icon}
	}
	return out
}

func projectDelta(delta Delta) DeltaResponse {
	return DeltaResponse{Pct: delta.Pct, Direction: delta.Direction, HasPrior: delta.HasPrior}
}

func projectNudges(nudges []nudge.Nudge) []NudgeResponse {
	out := make([]NudgeResponse, len(nudges))
	for i, item := range nudges {
		out[i] = NudgeResponse{ID: item.ID, Kind: item.Kind, Title: item.Title, Body: item.Body, Href: item.Href, CreatedAt: item.CreatedAt}
	}
	return out
}

func briefingText(briefing *report.Report) string {
	if briefing == nil || !briefing.Ready() {
		return ""
	}
	return briefing.Body
}
