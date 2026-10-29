package profiles

import (
	"context"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/awards"
	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
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

// Register goes behind auth.Bearer.
func (a *API) Register(g *echo.Group) {
	g.GET("/profiles/:handle", a.profile)
	g.GET("/profiles/:handle/workouts", a.workouts)
	g.GET("/profiles/:handle/workouts/:sessionID", a.workout)
}

// SharesView is which sections of the profile this viewer can see.
type SharesView struct {
	Training bool `json:"training"`
	XP       bool `json:"xp"`
	// Awards is optional on the wire: a client from before badges ignores it.
	Awards *bool `json:"awards,omitempty"`
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
	// Awards are the badges shown, absent when none are or the person keeps
	// them to themselves.
	Awards []award.View `json:"awards,omitempty"`
	// MonthlyChallenge is the current month's challenge, only when the person
	// shares awards. Their monthly medals are in Awards, with the badges.
	MonthlyChallenge *award.MonthlyChallengeView `json:"monthlyChallenge,omitempty"`
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
	awardsShared := v.Shares.Awards
	out := ProfileView{
		ProfileView: social.ProjectProfile(v.Profile),
		Shares:      &SharesView{Training: v.Shares.Training, XP: v.Shares.XP, Awards: &awardsShared},
		Workouts:    v.Workouts,
	}
	if v.Level != nil {
		l := xp.ProjectLevel(ctx, *v.Level)
		out.Level = &l
	}
	if v.Recent != nil {
		out.RecentWorkouts = projectCards(v.Recent, 2)
	}
	var badges []award.View
	for _, e := range v.Awards {
		at := e.At
		badges = append(badges, e.Badge.Project(ctx, &at, nil, ""))
	}
	out.Awards = badges
	if len(v.Monthly) > 0 {
		out.Awards = awards.MergeEarned(badges, awards.ProjectMonthlyMedals(ctx, v.Monthly))
	}
	if v.MonthlyChallenge != nil {
		c := awards.ProjectMonthlyChallenge(ctx, *v.MonthlyChallenge)
		out.MonthlyChallenge = &c
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

func (a *API) profile(c *echo.Context) error {
	ctx := c.Request().Context()
	v, err := a.svc.View(ctx, auth.MustUser(ctx), c.Param("handle"), a.now())
	if err != nil {
		return httpx.Fail(err, "Nobody has that handle.")
	}
	return c.JSON(http.StatusOK, projectView(ctx, v))
}

func (a *API) workouts(c *echo.Context) error {
	var before time.Time
	if raw := c.QueryParam("before"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return httpx.Fail(apperr.FieldErrors{}.Add("before", "Use a time like 2026-10-22T09:00:00Z."), "Check the time.")
		}
		before = t
	}
	ctx := c.Request().Context()
	p, err := a.svc.Workouts(ctx, auth.MustUser(ctx), c.Param("handle"), before)
	if err != nil {
		return httpx.Fail(err, "Nobody has that handle.")
	}
	return c.JSON(http.StatusOK, projectPage(p))
}

func (a *API) workout(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "sessionID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	card, err := a.svc.Workout(ctx, auth.MustUser(ctx), c.Param("handle"), id)
	if err != nil {
		return httpx.Fail(err, "Not found.")
	}
	return c.JSON(http.StatusOK, lifts.ProjectCard(card, 0))
}
