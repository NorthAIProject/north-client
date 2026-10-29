package awards

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/awards/award"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the caller's own badges for native clients. Mount behind
// auth.RequireBearer.
type API struct {
	svc    *Service
	people People
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

// WithPeople resolves handles, for another person's awards and for the
// caller's own handle on GET /awards. Without it those are absent.
func (a *API) WithPeople(p People) *API { a.people = p; return a }

func (a *API) Routes(r chi.Router) {
	r.Get("/awards", a.list)
	r.Get("/awards/unseen", a.unseen)
	r.Post("/awards/{badgeID}/seen", a.seen)
	r.Get("/profiles/{handle}/awards", a.person)
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

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := auth.MustUser(ctx).ID
	earned, err := a.svc.Earned(ctx, userID)
	if err != nil {
		httpx.Error(w, err, "Your awards could not be loaded.")
		return
	}
	progress, err := a.svc.Progress(ctx, userID)
	if err != nil {
		httpx.Error(w, err, "Your awards could not be loaded.")
		return
	}
	rarities, err := a.svc.Rarities(ctx)
	if err != nil {
		httpx.Error(w, err, "Your awards could not be loaded.")
		return
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
			httpx.Error(w, err, "Your awards could not be loaded.")
			return
		}
		out.Handle = handle
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// person is GET /profiles/{handle}/awards: the web's /app/u/{handle}/awards.
// An unknown handle or a block either way is 404, as on the profile.
func (a *API) person(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if a.people == nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	viewer := auth.MustUser(ctx)
	p, err := a.people.Person(ctx, viewer, chi.URLParam(r, "handle"))
	if err != nil {
		httpx.Error(w, err, "Nobody has that handle.")
		return
	}
	out := PersonAwardsView{
		DisplayName: p.Name, Handle: p.Handle, Self: p.Self, Shared: p.Self || p.Shared,
		Earned: []award.View{}, Locked: []award.View{},
	}
	if !out.Shared {
		httpx.WriteJSON(w, http.StatusOK, out)
		return
	}
	earned, err := a.svc.Earned(ctx, p.ID)
	if err != nil {
		httpx.Error(w, err, "The awards could not be loaded.")
		return
	}
	rarities, err := a.svc.Rarities(ctx)
	if err != nil {
		httpx.Error(w, err, "The awards could not be loaded.")
		return
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
		if c, err := a.svc.Monthly(ctx, p.ID, a.svc.now()); err != nil {
			a.svc.log.Warn("awards: monthly challenge skipped", "user_id", p.ID, "error", err)
		} else {
			cv := ProjectMonthlyChallenge(ctx, c)
			out.MonthlyChallenge = &cv
		}
		progress, err := a.svc.Progress(ctx, p.ID)
		if err != nil {
			httpx.Error(w, err, "The awards could not be loaded.")
			return
		}
		for _, b := range award.Catalog() {
			if held[b.ID] {
				continue
			}
			n := min(progress[b.ID], b.Target)
			out.Locked = append(out.Locked, b.Project(ctx, nil, &n, rarities[b.ID].Label()))
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) unseen(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	unseen, err := a.svc.Unseen(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		httpx.Error(w, err, "Your awards could not be loaded.")
		return
	}
	rarities, err := a.svc.Rarities(ctx)
	if err != nil {
		httpx.Error(w, err, "Your awards could not be loaded.")
		return
	}
	medals, err := a.svc.UnseenMonthly(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		httpx.Error(w, err, "Your awards could not be loaded.")
		return
	}
	out := UnseenView{Awards: make([]award.View, 0, len(unseen)+len(medals))}
	for _, e := range unseen {
		out.Awards = append(out.Awards, projectEarned(ctx, e, rarities))
	}
	// Medals go after the badges, oldest first like the rest of the list.
	out.Awards = append(out.Awards, ProjectMonthlyMedals(ctx, medals)...)
	httpx.WriteJSON(w, http.StatusOK, out)
}

// seen marks a badge celebrated. A badge that does not exist, or that the
// caller has not earned, is not found; one already seen is a repeat and still
// 204, so a client may retry a seen it is unsure reached us.
func (a *API) seen(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := auth.MustUser(ctx).ID
	id := chi.URLParam(r, "badgeID")
	if key, ok := award.ParseMonthlyID(id); ok {
		if err := a.svc.MarkMonthlySeen(ctx, userID, key); err != nil {
			httpx.Error(w, err, "Not found.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if _, ok := award.ByID(id); !ok {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	earned, err := a.svc.Earned(ctx, userID)
	if err != nil {
		httpx.Error(w, err, "That could not be saved.")
		return
	}
	held := false
	for _, e := range earned {
		held = held || e.Badge.ID == id
	}
	if !held {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	if err := a.svc.MarkSeen(ctx, userID, id); err != nil {
		httpx.Error(w, err, "That could not be saved.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
