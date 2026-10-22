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
}

// UnseenView is the badges not yet celebrated on any device.
type UnseenView struct {
	Awards []award.View `json:"awards"`
}

func projectEarned(ctx context.Context, e Earned, rarities map[string]Rarity) award.View {
	at := e.At
	return e.Badge.Project(ctx, &at, nil, rarities[e.Badge.ID].Label())
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
	out := AwardsView{Earned: []award.View{}, Locked: []award.View{}}
	held := map[string]bool{}
	for _, e := range earned {
		held[e.Badge.ID] = true
		out.Earned = append(out.Earned, projectEarned(ctx, e, rarities))
	}
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
	out := UnseenView{Awards: make([]award.View, 0, len(unseen))}
	for _, e := range unseen {
		out.Awards = append(out.Awards, projectEarned(ctx, e, rarities))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// seen marks a badge celebrated. A badge that does not exist, or that the
// caller has not earned, is not found; one already seen is a repeat and still
// 204, so a client may retry a seen it is unsure reached us.
func (a *API) seen(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := auth.MustUser(ctx).ID
	id := chi.URLParam(r, "badgeID")
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
