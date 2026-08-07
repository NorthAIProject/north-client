package mind

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	mindpages "github.com/NorthAIProject/north-client/web/mind"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/mind", h.index)
	r.Post("/mind/journal", h.create)
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	entries, err := h.svc.Recent(r.Context(), user.ID, 50)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	trend, err := h.svc.RecentMoodTrend(r.Context(), user.ID, 14)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, mindpages.IndexPage(user, entries, trend.AverageMood, trend.AverageEnergy, trend.Count, mindpages.JournalForm{}))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := mindpages.JournalForm{Content: r.PostFormValue("content")}

	var mood *int
	if raw := strings.TrimSpace(r.PostFormValue("mood")); raw != "" {
		if n, convErr := strconv.Atoi(raw); convErr == nil {
			mood = &n
		}
	}
	form.Mood = mood

	if _, err := h.svc.Create(r.Context(), user.ID, Input{Content: form.Content, Mood: mood}); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()

			entries, listErr := h.svc.Recent(r.Context(), user.ID, 50)
			if listErr != nil {
				h.fail(w, r, listErr)
				return
			}
			trend, trendErr := h.svc.RecentMoodTrend(r.Context(), user.ID, 14)
			if trendErr != nil {
				h.fail(w, r, trendErr)
				return
			}
			h.render(w, r, http.StatusUnprocessableEntity, mindpages.IndexPage(user, entries, trend.AverageMood, trend.AverageEnergy, trend.Count, form))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/mind", http.StatusSeeOther)
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("mind request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}
