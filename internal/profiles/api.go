package profiles

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/xp"
)

// API is a person's public profile for native clients.
type API struct {
	svc *Service
	now func() time.Time
}

func NewAPI(svc *Service) *API { return &API{svc: svc, now: time.Now} }

// Routes go behind auth.RequireBearer.
func (a *API) Routes(r chi.Router) {
	r.Get("/profiles/{handle}", a.profile)
	r.Get("/profiles/{handle}/workouts", a.workouts)
	r.Get("/profiles/{handle}/workouts/{sessionID}", a.workout)
}

// SharesView is which sections of the profile this viewer can see.
type SharesView struct {
	Training bool `json:"training"`
	XP       bool `json:"xp"`
}

// ProfileView is everything social.ProfileView already carried, under the same
// names, plus what the person shares. The additions are all optional so a
// client from before them keeps decoding.
type ProfileView struct {
	social.ProfileView
	Shares   *SharesView   `json:"shares,omitempty"`
	Level    *xp.LevelView `json:"level,omitempty"`
	Workouts *int          `json:"workouts,omitempty"`
	// RecentWorkouts is absent, not empty, for someone with nothing to show.
	RecentWorkouts []lifts.WorkoutCardView `json:"recentWorkouts,omitempty"`
}

// WorkoutsView is a page of someone's workouts.
type WorkoutsView struct {
	Items  []lifts.WorkoutCardView `json:"items"`
	Before *time.Time              `json:"before,omitempty"`
	// Shared is false when the person does not share workouts; items is then
	// empty rather than missing.
	Shared bool `json:"shared"`
}

func projectView(ctx context.Context, v View) ProfileView {
	out := ProfileView{
		ProfileView: social.ProjectProfile(v.Profile),
		Shares:      &SharesView{Training: v.Shares.Training, XP: v.Shares.XP},
		Workouts:    v.Workouts,
	}
	if v.Level != nil {
		l := xp.ProjectLevel(ctx, *v.Level)
		out.Level = &l
	}
	if v.Recent != nil {
		out.RecentWorkouts = projectCards(v.Recent, 2)
	}
	return out
}

func projectCards(cards []lift.Card, limit int) []lifts.WorkoutCardView {
	out := make([]lifts.WorkoutCardView, 0, len(cards))
	for _, c := range cards {
		out = append(out, lifts.ProjectCard(c, limit))
	}
	return out
}

func projectPage(p Page) WorkoutsView {
	return WorkoutsView{Items: projectCards(p.Items, 2), Before: p.Before, Shared: p.Shared}
}

func (a *API) profile(w http.ResponseWriter, r *http.Request) {
	v, err := a.svc.View(r.Context(), auth.MustUser(r.Context()), chi.URLParam(r, "handle"), a.now())
	if err != nil {
		httpx.Error(w, err, "Nobody has that handle.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectView(r.Context(), v))
}

func (a *API) workouts(w http.ResponseWriter, r *http.Request) {
	var before time.Time
	if raw := r.URL.Query().Get("before"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			httpx.Error(w, apperr.FieldErrors{}.Add("before", "Use a time like 2026-10-22T09:00:00Z."), "Check the time.")
			return
		}
		before = t
	}
	p, err := a.svc.Workouts(r.Context(), auth.MustUser(r.Context()), chi.URLParam(r, "handle"), before)
	if err != nil {
		httpx.Error(w, err, "Nobody has that handle.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectPage(p))
}

func (a *API) workout(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	c, err := a.svc.Workout(r.Context(), auth.MustUser(r.Context()), chi.URLParam(r, "handle"), id)
	if err != nil {
		httpx.Error(w, err, "Not found.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, lifts.ProjectCard(c, 0))
}
