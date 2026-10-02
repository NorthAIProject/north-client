package inbox

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/inbox/item"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the capture inbox for native clients and the share extension.
// Mount behind auth.RequireBearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/inbox", a.list)
	r.Post("/inbox", a.add)
	r.Post("/inbox/{itemID}/file", a.file)
	r.Post("/inbox/{itemID}/dismiss", a.dismiss)
}

type SuggestionView struct {
	// Destination is goal_note, knowledge or journal.
	Destination string     `json:"destination"`
	GoalID      *uuid.UUID `json:"goalId,omitempty"`
	GoalTitle   string     `json:"goalTitle,omitempty"`
	// Title is a suggested knowledge-note title.
	Title string `json:"title,omitempty"`
	Why   string `json:"why"`
}

type ItemView struct {
	ID     uuid.UUID `json:"id"`
	Text   string    `json:"text"`
	Source string    `json:"source"`
	// Suggestion is absent while the coach has not looked at it yet.
	Suggestion *SuggestionView `json:"suggestion,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
}

type InboxView struct {
	Items []ItemView `json:"items"`
	// Open is how many are waiting, which can be more than items lists.
	Open int `json:"open"`
}

type AddRequest struct {
	Text string `json:"text"`
	// Source is app, share, shortcut or web; app when absent.
	Source string `json:"source"`
}

type FileRequest struct {
	Destination string    `json:"destination"`
	GoalID      uuid.UUID `json:"goalId"`
	Title       string    `json:"title"`
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	items, open, err := a.svc.Open(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "The inbox could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectInbox(items, open))
}

func (a *API) add(w http.ResponseWriter, r *http.Request) {
	var req AddRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.Source == "" {
		req.Source = item.SourceApp
	}
	it, err := a.svc.Add(r.Context(), auth.MustUser(r.Context()).ID, req.Source, req.Text)
	if err != nil {
		httpx.Error(w, err, "That could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, projectItem(it))
}

func (a *API) file(w http.ResponseWriter, r *http.Request) {
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	var req FileRequest
	if !readJSON(w, r, &req) {
		return
	}
	it, err := a.svc.File(r.Context(), auth.MustUser(r.Context()).ID, id, Filing(req))
	if err != nil {
		httpx.Error(w, err, "That could not be filed.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectItem(it))
}

func (a *API) dismiss(w http.ResponseWriter, r *http.Request) {
	id, ok := itemID(w, r)
	if !ok {
		return
	}
	if err := a.svc.Dismiss(r.Context(), auth.MustUser(r.Context()).ID, id); err != nil {
		httpx.Error(w, err, "That could not be dismissed.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func projectInbox(items []Item, open int) InboxView {
	out := InboxView{Items: make([]ItemView, 0, len(items)), Open: open}
	for _, it := range items {
		out.Items = append(out.Items, projectItem(it))
	}
	return out
}

func projectItem(it Item) ItemView {
	v := ItemView{ID: it.ID, Text: it.Text, Source: it.Source, CreatedAt: it.CreatedAt}
	if s := it.Suggestion; s != nil {
		v.Suggestion = &SuggestionView{Destination: s.Destination, GoalID: s.GoalID, GoalTitle: s.GoalTitle, Title: s.Title, Why: s.Why}
	}
	return v
}

func itemID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "itemID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return uuid.Nil, false
	}
	return id, true
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := httpx.ReadJSON(w, r, dst, httpx.ReadOptions{MaxBytes: 32 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return false
	}
	return true
}
