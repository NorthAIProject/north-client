package media

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	formpages "github.com/NorthAIProject/north-client/web/form"

	"github.com/a-h/templ"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mounts the form-analysis endpoints. Must be behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/form", h.index)
	r.Post("/form", h.upload)
	r.Get("/form/{id}", h.show)
	r.Get("/form/{id}/status", h.status)
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	analyses, err := h.svc.ListAnalyses(r.Context(), user.ID, 20)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusOK, formpages.IndexPage(user, analyses, ""))
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	// Caps what the process will read, before any of it is buffered. Without
	// this a large upload is absorbed and only then rejected.
	r.Body = http.MaxBytesReader(w, r.Body, MaxVideoBytes+(1<<20))

	file, header, err := r.FormFile("video")
	if err != nil {
		// Covers both "no file chosen" and a body that exceeded MaxBytesReader,
		// which surfaces here as a failed multipart parse.
		h.uploadFailed(w, r, "Choose a video to upload. Clips must be under 200 MB.")
		return
	}
	defer file.Close()

	pending, err := h.svc.UploadVideo(r.Context(), user.ID, header.Filename, header.Size, file)
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			h.uploadFailed(w, r, fieldErrs.Messages()["video"])
			return
		}

		middleware.FromContext(r.Context()).Error("video upload failed", slog.Any("error", err))
		h.uploadFailed(w, r, "Something went wrong storing that video. Try again.")
		return
	}

	http.Redirect(w, r, "/app/form/"+pending.ID.String(), http.StatusSeeOther)
}

// uploadFailed re-renders the upload page with the reason.
func (h *Handler) uploadFailed(w http.ResponseWriter, r *http.Request, message string) {
	user := auth.MustUser(r.Context())

	analyses, err := h.svc.ListAnalyses(r.Context(), user.ID, 20)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusUnprocessableEntity, formpages.IndexPage(user, analyses, message))
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	item, err := h.svc.GetAnalysis(r.Context(), id, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	clip, err := h.svc.GetMedia(r.Context(), item.MediaID, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	playback, err := h.svc.PlaybackURL(r.Context(), clip)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusOK, formpages.AnalysisPage(user, item, playback))
}

// status is polled by the page while an analysis runs, and returns the fragment
// that replaces itself. Polling rather than SSE: the answer arrives once,
// minutes later, so a held-open connection buys nothing.
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	item, err := h.svc.GetAnalysis(r.Context(), id, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusOK, formpages.StatusFragment(item))
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("form analysis request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}

func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}
