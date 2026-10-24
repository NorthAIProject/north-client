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
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/awards", a.list)
	r.Get("/awards/unseen", a.unseen)
	r.Post("/awards/{badgeID}/seen", a.seen)
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
	medals, err := a.svc.MonthlyMedals(ctx, userID)
	if err != nil {
		httpx.Error(w, err, "Your awards could not be loaded.")
		return
	}
	// The current month only: a past month's unmet challenge has no place here.
	challenge, err := a.svc.Monthly(ctx, userID, a.svc.now())
	if err != nil {
		httpx.Error(w, err, "Your awards could not be loaded.")
		return
	}
	cv := ProjectMonthlyChallenge(ctx, challenge)
	out := AwardsView{Earned: []award.View{}, Locked: []award.View{}, MonthlyChallenge: &cv}
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
