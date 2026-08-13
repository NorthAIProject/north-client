package mind

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/checkins"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
	mindpages "github.com/NorthAIProject/north-client/web/mind"
)

type Handler struct {
	svc      *Service
	checkins *checkins.Service
}

func NewHandler(svc *Service, checkins *checkins.Service) *Handler {
	return &Handler{svc: svc, checkins: checkins}
}

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

	inst, err := h.loadInstruments(r, user)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, mindpages.IndexPage(user, entries, inst, mindpages.JournalForm{}))
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
			inst, instErr := h.loadInstruments(r, user)
			if instErr != nil {
				h.fail(w, r, instErr)
				return
			}
			h.render(w, r, http.StatusUnprocessableEntity, mindpages.IndexPage(user, entries, inst, form))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/mind", http.StatusSeeOther)
}

func (h *Handler) loadInstruments(r *http.Request, user users.User) (mindpages.Instruments, error) {
	list, err := h.checkins.RecentForContext(r.Context(), user)
	if err != nil {
		return mindpages.Instruments{}, err
	}
	return buildInstruments(user, list), nil
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
