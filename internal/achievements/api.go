package achievements

import (
	"net/http"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the friends feed for native clients, what followers may see, and
// kudos. Mount behind auth.RequireBearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/feed", a.feed)
	r.Get("/social/sharing", a.getSharing)
	r.Put("/social/sharing", a.putSharing)
	r.Post("/achievements/{achievementID}/kudos", a.giveKudos)
	r.Delete("/achievements/{achievementID}/kudos", a.takeKudos)
}

type FeedItem struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"userId"`
	DisplayName string    `json:"displayName"`
	Handle      string    `json:"handle"`
	// Category is training, streaks or goals.
	Category   string    `json:"category"`
	Kind       string    `json:"kind"`
	Title      string    `json:"title"`
	Detail     string    `json:"detail"`
	OccurredAt time.Time `json:"occurredAt"`
	Kudos      int       `json:"kudos"`
	Kudoed     bool      `json:"kudoed"`
	Mine       bool      `json:"mine"`
}

type Feed struct {
	Items []FeedItem `json:"items"`
	// Before is the cursor for the next page; absent on the last one.
	Before *time.Time `json:"before,omitempty"`
}

type SharingView struct {
	Training bool `json:"training"`
	Streaks  bool `json:"streaks"`
	Goals    bool `json:"goals"`
}

func (a *API) feed(w http.ResponseWriter, r *http.Request) {
	var before time.Time
	if raw := r.URL.Query().Get("before"); raw != "" {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			httpx.Error(w, apperr.FieldErrors{}.Add("before", "Use an RFC 3339 time."), "That page could not be read.")
			return
		}
		before = t
	}
	items, err := a.svc.Feed(r.Context(), auth.MustUser(r.Context()).ID, before)
	if err != nil {
		httpx.Error(w, err, "The feed could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectFeed(items))
}

func projectFeed(items []Item) Feed {
	out := Feed{Items: make([]FeedItem, 0, len(items))}
	for _, it := range items {
		out.Items = append(out.Items, FeedItem{
			ID: it.ID, UserID: it.UserID, DisplayName: it.DisplayName, Handle: it.Handle,
			Category: it.Category, Kind: it.Kind, Title: it.Title, Detail: it.Detail, OccurredAt: it.OccurredAt,
			Kudos: it.Kudos, Kudoed: it.Kudoed, Mine: it.Mine,
		})
	}
	if len(items) == feedPage {
		out.Before = util.Ptr(items[len(items)-1].OccurredAt)
	}
	return out
}

func (a *API) getSharing(w http.ResponseWriter, r *http.Request) {
	s, err := a.svc.Sharing(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "What friends see could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, SharingView(s))
}

func (a *API) putSharing(w http.ResponseWriter, r *http.Request) {
	var req SharingView
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	s, err := a.svc.SetSharing(r.Context(), auth.MustUser(r.Context()).ID, Sharing(req))
	if err != nil {
		httpx.Error(w, err, "What friends see could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, SharingView(s))
}

func (a *API) giveKudos(w http.ResponseWriter, r *http.Request) {
	id, ok := achievementID(w, r)
	if !ok {
		return
	}
	user := auth.MustUser(r.Context())
	if err := a.svc.GiveKudos(r.Context(), user.ID, id, user.DisplayName); err != nil {
		httpx.Error(w, err, "Kudos could not be given.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) takeKudos(w http.ResponseWriter, r *http.Request) {
	id, ok := achievementID(w, r)
	if !ok {
		return
	}
	if err := a.svc.TakeKudos(r.Context(), auth.MustUser(r.Context()).ID, id); err != nil {
		httpx.Error(w, err, "Kudos could not be taken back.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func achievementID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "achievementID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return uuid.Nil, false
	}
	return id, true
}
