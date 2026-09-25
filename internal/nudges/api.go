package nudges

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the web's notification bell for native clients: the open nudges,
// how many are unread, and opening, reading or dismissing one.
type API struct {
	svc *Service
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/nudges", a.list)
	r.Post("/nudges/{nudgeID}/open", a.open)
	r.Post("/nudges/{nudgeID}/read", a.read)
	r.Post("/nudges/{nudgeID}/dismiss", a.dismiss)
}

type NudgeView struct {
	ID    uuid.UUID `json:"id"`
	Kind  string    `json:"kind"`
	Title string    `json:"title"`
	Body  string    `json:"body"`
	// Href is the web path it leads to; the app maps it onto a screen.
	Href      string    `json:"href"`
	Unread    bool      `json:"unread"`
	CreatedAt time.Time `json:"createdAt"`
}

type NudgeList struct {
	Nudges []NudgeView `json:"nudges"`
	Unread int         `json:"unread"`
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUser(r.Context()).ID
	list, err := a.svc.ListOpen(r.Context(), userID, listDefault)
	if err != nil {
		httpx.Error(w, err, "Notifications could not be loaded.")
		return
	}
	unread, err := a.svc.CountUnread(r.Context(), userID)
	if err != nil {
		httpx.Error(w, err, "Notifications could not be loaded.")
		return
	}
	out := NudgeList{Nudges: make([]NudgeView, 0, len(list)), Unread: unread}
	for _, n := range list {
		out.Nudges = append(out.Nudges, project(n))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// open records which channel the nudge was followed from and marks it read.
// The bell is the default; ?from=push is the app opening a notification tap,
// so the funnel credits the lock screen and not the bell.
func (a *API) open(w http.ResponseWriter, r *http.Request) {
	id, ok := nudgeID(w, r)
	if !ok {
		return
	}
	channel := analytics.ChannelBell
	if r.URL.Query().Get("from") == analytics.ChannelPush {
		channel = analytics.ChannelPush
	}
	n, err := a.svc.Open(r.Context(), id, auth.MustUser(r.Context()).ID, channel)
	a.respond(w, n, err)
}

func (a *API) read(w http.ResponseWriter, r *http.Request) {
	id, ok := nudgeID(w, r)
	if !ok {
		return
	}
	n, err := a.svc.MarkRead(r.Context(), id, auth.MustUser(r.Context()).ID)
	a.respond(w, n, err)
}

func (a *API) dismiss(w http.ResponseWriter, r *http.Request) {
	id, ok := nudgeID(w, r)
	if !ok {
		return
	}
	n, err := a.svc.Dismiss(r.Context(), id, auth.MustUser(r.Context()).ID)
	a.respond(w, n, err)
}

func (a *API) respond(w http.ResponseWriter, n Nudge, err error) {
	if err != nil {
		httpx.Error(w, err, "The notification could not be updated.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, project(n))
}

func project(n Nudge) NudgeView {
	return NudgeView{ID: n.ID, Kind: n.Kind, Title: n.Title, Body: n.Body, Href: n.Href, Unread: n.Unread(), CreatedAt: n.CreatedAt}
}

func nudgeID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "nudgeID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return uuid.Nil, false
	}
	return id, true
}
