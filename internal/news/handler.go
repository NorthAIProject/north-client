package news

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	newspages "github.com/NorthAIProject/north-client/web/news"
)

// Handler serves the dashboard partial and the settings page. Thin: it reads
// the form, calls the service, renders.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Routes mounts under /app, behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/news-ticker", h.partial)
	r.Get("/settings/news-ticker", h.showSettings)
	r.Post("/settings/news-ticker", h.toggle)
	r.Post("/settings/news-ticker/feeds", h.addFeed)
	r.Post("/settings/news-ticker/feeds/{id}/delete", h.removeFeed)
}

// partial is the lazy-loaded strip. Off or empty renders nothing so the
// shell collapses; a failure renders nothing too — the strip is ambient.
func (h *Handler) partial(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	ticker, err := h.svc.Ticker(r.Context(), user, 12)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		middleware.FromContext(r.Context()).Warn("news ticker", slog.Any("error", err))
		return
	}
	if !ticker.Enabled || len(ticker.Items) == 0 {
		return
	}
	if err := newspages.TickerPanel(ticker.Items, time.Now()).Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render news ticker", slog.Any("error", err))
	}
}

func (h *Handler) showSettings(w http.ResponseWriter, r *http.Request) {
	h.renderSettings(w, r, newspages.FeedForm{})
}

func (h *Handler) toggle(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	// A checkbox switch posts "on" when checked and nothing at all when not.
	enabled := r.PostFormValue("enabled") != ""
	if err := h.svc.SetEnabled(r.Context(), user, enabled); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/settings/news-ticker", http.StatusSeeOther)
}

func (h *Handler) addFeed(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	form := newspages.FeedForm{URL: strings.TrimSpace(r.PostFormValue("url"))}
	if _, err := h.svc.AddFeed(r.Context(), user, form.URL); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.renderSettings(w, r, form)
			return
		}
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/settings/news-ticker", http.StatusSeeOther)
}

func (h *Handler) removeFeed(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	if err := h.svc.RemoveFeed(r.Context(), user, id); err != nil && !apperr.Is(err, apperr.ErrNotFound) {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/settings/news-ticker", http.StatusSeeOther)
}

func (h *Handler) renderSettings(w http.ResponseWriter, r *http.Request, form newspages.FeedForm) {
	user := auth.MustUser(r.Context())
	enabled, err := h.svc.Enabled(r.Context(), user)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	feeds, err := h.svc.Feeds(r.Context(), user)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := newspages.SettingsPage(user, newspages.SettingsData{
		Enabled: enabled, Feeds: feeds, MaxFeeds: h.svc.MaxUserFeeds, Form: form,
	})
	if err := page.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render news ticker settings", slog.Any("error", err))
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("news ticker request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}
