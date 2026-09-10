package activity

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
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
	r.Post("/activity/log", h.log)
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

// log records a session that already happened, from the "log a finished
// session" form. The form speaks in minutes, kilometres, and the person's
// local clock; the service speaks in durations, metres, and instants.
func (h *Handler) log(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	in, err := parseLogForm(r, user.Location())
	if err != nil {
		h.render(w, r, err.Error())
		return
	}

	if _, err := h.svc.Log(r.Context(), user.ID, in); err != nil {
		if apperr.Is(err, apperr.ErrValidation) {
			h.render(w, r, err.Error())
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/activity", http.StatusSeeOther)
}

// datetimeLocal is the format a <input type="datetime-local"> submits: no
// zone, because the browser reports the person's wall clock.
const datetimeLocal = "2006-01-02T15:04"

func parseLogForm(r *http.Request, loc *time.Location) (LogInput, error) {
	in := LogInput{ActivityCode: r.PostFormValue("activity_code")}

	minutes, err := strconv.Atoi(strings.TrimSpace(r.PostFormValue("minutes")))
	if err != nil || minutes <= 0 {
		return LogInput{}, apperr.Wrap(apperr.ErrValidation, "say how many minutes the session lasted")
	}
	in.Duration = time.Duration(minutes) * time.Minute

	if raw := strings.TrimSpace(r.PostFormValue("distance_km")); raw != "" {
		km, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return LogInput{}, apperr.Wrap(apperr.ErrValidation, "distance must be a number of kilometres")
		}
		in.DistanceM = km * 1000
	}

	if raw := strings.TrimSpace(r.PostFormValue("started_at")); raw != "" {
		started, err := time.ParseInLocation(datetimeLocal, raw, loc)
		if err != nil {
			return LogInput{}, apperr.Wrap(apperr.ErrValidation, "start time must be a date and time")
		}
		in.StartedAt = started
	}

	return in, nil
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
