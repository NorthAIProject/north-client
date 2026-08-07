package activity

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	activitypages "github.com/NorthAIProject/north-client/web/activity"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/activity", h.show)
	r.Post("/activity/start", h.start)
	r.Post("/activity/{id}/pause", h.pause)
	r.Post("/activity/{id}/resume", h.resume)
	r.Post("/activity/{id}/stop", h.stop)
	r.Post("/activity/{id}/cancel", h.cancel)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "")
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	if _, err := h.svc.Start(r.Context(), user.ID, r.PostFormValue("activity_code")); err != nil {
		if apperr.Is(err, apperr.ErrValidation) || apperr.Is(err, apperr.ErrConflict) {
			h.render(w, r, err.Error())
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/activity", http.StatusSeeOther)
}

func (h *Handler) pause(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, func(userID, id uuid.UUID) error {
		_, err := h.svc.Pause(r.Context(), id, userID)
		return err
	})
}

func (h *Handler) resume(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, func(userID, id uuid.UUID) error {
		_, err := h.svc.Resume(r.Context(), id, userID)
		return err
	})
}

func (h *Handler) stop(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, func(userID, id uuid.UUID) error {
		_, err := h.svc.Stop(r.Context(), id, userID)
		return err
	})
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	h.transition(w, r, func(userID, id uuid.UUID) error {
		return h.svc.Cancel(r.Context(), id, userID)
	})
}

func (h *Handler) transition(w http.ResponseWriter, r *http.Request, do func(userID, id uuid.UUID) error) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	if err := do(user.ID, id); err != nil {
		if apperr.Is(err, apperr.ErrNotFound) || apperr.Is(err, apperr.ErrConflict) {
			h.render(w, r, err.Error())
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/activity", http.StatusSeeOther)
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, errMsg string) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	active, hasActive, err := h.svc.Active(ctx, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	recent, err := h.svc.List(ctx, user.ID, 20)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	since := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), 0, 0, 0, 0, user.Location())
	total, err := h.svc.TotalCaloriesSince(ctx, user.ID, since)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := activitypages.Page(user, active, hasActive, recent, total, errMsg).Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render activity", slog.Any("error", err))
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	middleware.FromContext(r.Context()).Error("activity request failed", slog.Any("error", err))
	http.Error(w, "Something went wrong.", http.StatusInternalServerError)
}
