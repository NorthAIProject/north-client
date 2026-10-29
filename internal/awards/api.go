package awards

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/awards/award"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the caller's own badges for native clients. Register behind
// auth.Bearer.
type API struct {
	svc    *Service
	people People
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

// WithPeople resolves handles, for another person's awards and for the
// caller's own handle on GET /awards. Without it those are absent.
func (a *API) WithPeople(p People) *API { a.people = p; return a }

func (a *API) Register(g *echo.Group) {
	g.GET("/awards", a.list)
	g.GET("/awards/unseen", a.unseen)
	g.POST("/awards/:badgeID/seen", a.seen)
	g.GET("/profiles/:handle/awards", a.person)
}

// AwardsView is every badge, split into what the caller holds and what is
// still to earn. Locked ones carry progress.
type AwardsView struct {
	Earned []award.View `json:"earned"`
	Locked []award.View `json:"locked"`
	// MonthlyChallenge is the current local month's challenge. Earned also
	// carries the monthly medals, with the badges, oldest first. A past
	// month's unmet challenge is never sent.
	MonthlyChallenge *award.MonthlyChallengeView `json:"monthlyChallenge,omitempty"`
	// Handle is the caller's own handle, absent when they have none.
	Handle string `json:"handle,omitempty"`
}

// PersonAwardsView is one person's awards page as the caller may see it:
// the owner gets locked badges and the month's challenge too; anyone else
// gets the public badges and medals, and only when the person shares them.
type PersonAwardsView struct {
	DisplayName string `json:"displayName"`
	Handle      string `json:"handle"`
	Self        bool   `json:"self"`
	// Shared is whether the person shows their awards; always true for the
	// owner. When false, earned and locked are empty.
	Shared           bool                        `json:"shared"`
	Earned           []award.View                `json:"earned"`
	Locked           []award.View                `json:"locked"`
	MonthlyChallenge *award.MonthlyChallengeView `json:"monthlyChallenge,omitempty"`
}

// UnseenView is the badges not yet celebrated on any device.
type UnseenView struct {
	Awards []award.View `json:"awards"`
}

func projectEarned(ctx context.Context, e Earned, rarities map[string]Rarity) award.View {
	at := e.At
	return e.Badge.Project(ctx, &at, nil, rarities[e.Badge.ID].WithHolder().Label())
}

func (a *API) list(c *echo.Context) error {
	ctx := c.Request().Context()
	userID := auth.MustUser(ctx).ID
	earned, err := a.svc.Earned(ctx, userID)
	if err != nil {
		return httpx.Fail(err, "Your awards could not be loaded.")
	}
	progress, err := a.svc.Progress(ctx, userID)
	if err != nil {
		return httpx.Fail(err, "Your awards could not be loaded.")
	}
	rarities, err := a.svc.Rarities(ctx)
	if err != nil {
		return httpx.Fail(err, "Your awards could not be loaded.")
	}
	// Monthly is a bonus: if it cannot be read, the badges still go out.
	medals, err := a.svc.MonthlyMedals(ctx, userID)
	if err != nil {
		a.svc.log.Warn("awards: monthly medals skipped", "user_id", userID, "error", err)
		medals = nil
	}
	out := AwardsView{Earned: []award.View{}, Locked: []award.View{}}
	// The current month only: a past month's unmet challenge has no place here.
	if challenge, err := a.svc.Monthly(ctx, userID, a.svc.now()); err != nil {
		a.svc.log.Warn("awards: monthly challenge skipped", "user_id", userID, "error", err)
	} else {
		cv := ProjectMonthlyChallenge(ctx, challenge)
		out.MonthlyChallenge = &cv
	}
	held := map[string]bool{}
	badges := make([]award.View, 0, len(earned))
	for _, e := range earned {
		held[e.Badge.ID] = true
		badges = append(badges, projectEarned(ctx, e, rarities))
	}
	out.Earned = MergeEarned(badges, ProjectMonthlyMedals(ctx, medals))
	for _, b := range award.Catalog() {
		if held[b.ID] {
			continue
		}
		n := min(progress[b.ID], b.Target)
		out.Locked = append(out.Locked, b.Project(ctx, nil, &n, rarities[b.ID].Label()))
	}
	if a.people != nil {
		handle, err := a.people.HandleOf(ctx, userID)
		if err != nil {
			return httpx.Fail(err, "Your awards could not be loaded.")
		}
		out.Handle = handle
	}
	return c.JSON(http.StatusOK, out)
}

// person is GET /profiles/:handle/awards: the web's /app/u/{handle}/awards.
// An unknown handle or a block either way is 404, as on the profile.
func (a *API) person(c *echo.Context) error {
	ctx := c.Request().Context()
	if a.people == nil {
		return httpx.Fail(apperr.ErrNotFound, "Not found.")
	}
	viewer := auth.MustUser(ctx)
	p, err := a.people.Person(ctx, viewer, c.Param("handle"))
	if err != nil {
		return httpx.Fail(err, "Nobody has that handle.")
	}
	out := PersonAwardsView{
		DisplayName: p.Name, Handle: p.Handle, Self: p.Self, Shared: p.Self || p.Shared,
		Earned: []award.View{}, Locked: []award.View{},
	}
	if !out.Shared {
		return c.JSON(http.StatusOK, out)
	}
	earned, err := a.svc.Earned(ctx, p.ID)
	if err != nil {
		return httpx.Fail(err, "The awards could not be loaded.")
	}
	rarities, err := a.svc.Rarities(ctx)
	if err != nil {
		return httpx.Fail(err, "The awards could not be loaded.")
	}
	held := map[string]bool{}
	badges := make([]award.View, 0, len(earned))
	for _, e := range earned {
		held[e.Badge.ID] = true
		// A private badge never leaves its owner, whatever the switch.
		if p.Self || !e.Badge.Area.Private() {
			badges = append(badges, projectEarned(ctx, e, rarities))
		}
	}
	// Monthly is a bonus: a failed read hides the medals and the card.
	medals, err := a.svc.MonthlyMedals(ctx, p.ID)
	if err != nil {
		a.svc.log.Warn("awards: monthly medals skipped", "user_id", p.ID, "error", err)
		medals = nil
	}
	out.Earned = MergeEarned(badges, ProjectMonthlyMedals(ctx, medals))
	if p.Self {
		if challenge, err := a.svc.Monthly(ctx, p.ID, a.svc.now()); err != nil {
			a.svc.log.Warn("awards: monthly challenge skipped", "user_id", p.ID, "error", err)
		} else {
			cv := ProjectMonthlyChallenge(ctx, challenge)
			out.MonthlyChallenge = &cv
		}
		progress, err := a.svc.Progress(ctx, p.ID)
		if err != nil {
			return httpx.Fail(err, "The awards could not be loaded.")
		}
		for _, b := range award.Catalog() {
			if held[b.ID] {
				continue
			}
			n := min(progress[b.ID], b.Target)
			out.Locked = append(out.Locked, b.Project(ctx, nil, &n, rarities[b.ID].Label()))
		}
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) unseen(c *echo.Context) error {
	ctx := c.Request().Context()
	unseen, err := a.svc.Unseen(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "Your awards could not be loaded.")
	}
	rarities, err := a.svc.Rarities(ctx)
	if err != nil {
		return httpx.Fail(err, "Your awards could not be loaded.")
	}
	medals, err := a.svc.UnseenMonthly(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "Your awards could not be loaded.")
	}
	out := UnseenView{Awards: make([]award.View, 0, len(unseen)+len(medals))}
	for _, e := range unseen {
		out.Awards = append(out.Awards, projectEarned(ctx, e, rarities))
	}
	// Medals go after the badges, oldest first like the rest of the list.
	out.Awards = append(out.Awards, ProjectMonthlyMedals(ctx, medals)...)
	return c.JSON(http.StatusOK, out)
}

// seen marks a badge celebrated. A badge that does not exist, or that the
// caller has not earned, is not found; one already seen is a repeat and still
// 204, so a client may retry a seen it is unsure reached us.
func (a *API) seen(c *echo.Context) error {
	ctx := c.Request().Context()
	userID := auth.MustUser(ctx).ID
	id := c.Param("badgeID")
	if key, ok := award.ParseMonthlyID(id); ok {
		if err := a.svc.MarkMonthlySeen(ctx, userID, key); err != nil {
			return httpx.Fail(err, "Not found.")
		}
		return c.NoContent(http.StatusNoContent)
	}
	if _, ok := award.ByID(id); !ok {
		return httpx.Fail(apperr.ErrNotFound, "Not found.")
	}
	earned, err := a.svc.Earned(ctx, userID)
	if err != nil {
		return httpx.Fail(err, "That could not be saved.")
	}
	held := false
	for _, e := range earned {
		held = held || e.Badge.ID == id
	}
	if !held {
		return httpx.Fail(apperr.ErrNotFound, "Not found.")
	}
	if err := a.svc.MarkSeen(ctx, userID, id); err != nil {
		return httpx.Fail(err, "That could not be saved.")
	}
	return c.NoContent(http.StatusNoContent)
}
