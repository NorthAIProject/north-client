package news

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// The ticker's settings, for web clients over /api/v1: the switch and the
// feeds the person follows, which the cookie-session settings page serves at
// /app/settings/news-ticker.

type FeedView struct {
	ID uuid.UUID `json:"id"`
	// Title is the feed's own name once fetched; empty before.
	Title string `json:"title"`
	URL   string `json:"url"`
}

type NewsSettings struct {
	Enabled  bool       `json:"enabled"`
	Feeds    []FeedView `json:"feeds"`
	MaxFeeds int        `json:"maxFeeds"`
}

type newsSettingsInput struct {
	Enabled bool `json:"enabled"`
}

type feedInput struct {
	URL string `json:"url"`
}

func (a *API) settingsRoutes(r chi.Router) {
	r.Get("/news/settings", a.settings)
	r.Put("/news/settings", a.setSettings)
	r.Post("/news/feeds", a.addFeed)
	r.Delete("/news/feeds/{feedID}", a.removeFeed)
}

func (a *API) settings(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	a.writeSettings(w, r, http.StatusOK)
}

func (a *API) setSettings(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	var in newsSettingsInput
	if err := httpx.ReadJSON(w, r, &in, httpx.ReadOptions{MaxBytes: 1 << 10}); err != nil {
		httpx.Error(w, apperr.ErrValidation, "That request could not be read.")
		return
	}
	if err := a.svc.SetEnabled(r.Context(), auth.MustUser(r.Context()), in.Enabled); err != nil {
		httpx.Error(w, err, "The news settings could not be saved.")
		return
	}
	a.writeSettings(w, r, http.StatusOK)
}

// addFeed answers 422 with fields.url when the address is refused, as the
// settings form shows it.
func (a *API) addFeed(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	var in feedInput
	if err := httpx.ReadJSON(w, r, &in, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, apperr.ErrValidation, "That request could not be read.")
		return
	}
	if _, err := a.svc.AddFeed(r.Context(), auth.MustUser(r.Context()), strings.TrimSpace(in.URL)); err != nil {
		httpx.Error(w, err, "That feed could not be added.")
		return
	}
	a.writeSettings(w, r, http.StatusCreated)
}

// removeFeed treats a feed that is already gone as removed.
func (a *API) removeFeed(w http.ResponseWriter, r *http.Request) {
	if !a.available(w) {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "feedID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	if err := a.svc.RemoveFeed(r.Context(), auth.MustUser(r.Context()), id); err != nil && !apperr.Is(err, apperr.ErrNotFound) {
		httpx.Error(w, err, "That feed could not be removed.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) writeSettings(w http.ResponseWriter, r *http.Request, status int) {
	user := auth.MustUser(r.Context())
	enabled, err := a.svc.Enabled(r.Context(), user)
	if err != nil {
		httpx.Error(w, err, "The news settings could not be loaded.")
		return
	}
	feeds, err := a.svc.Feeds(r.Context(), user)
	if err != nil {
		httpx.Error(w, err, "The news settings could not be loaded.")
		return
	}
	out := NewsSettings{Enabled: enabled, Feeds: make([]FeedView, 0, len(feeds)), MaxFeeds: a.svc.MaxUserFeeds}
	for _, f := range feeds {
		out.Feeds = append(out.Feeds, FeedView{ID: f.ID, Title: f.Title, URL: f.URL})
	}
	httpx.WriteJSON(w, status, out)
}

// available answers 404 on a deployment without feeds configured.
func (a *API) available(w http.ResponseWriter) bool {
	if a.svc == nil {
		httpx.Error(w, apperr.ErrNotFound, "News is not available here.")
		return false
	}
	return true
}
