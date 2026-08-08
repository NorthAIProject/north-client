package documents

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	knowledgepages "github.com/NorthAIProject/north-client/web/knowledge"
)

// maxMultipartMemory bounds what an upload buffers in RAM before spilling to a
// temporary file. The service enforces the real size limit.
const maxMultipartMemory = 4 << 20 // 4 MiB

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mounts the knowledge page. Must be behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/knowledge", h.index)
	r.Post("/knowledge/notes", h.createNote)
	r.Post("/knowledge/uploads", h.upload)
	r.Post("/knowledge/reindex", h.reindex)
	r.Post("/knowledge/{id}/delete", h.destroy)

	// The export route is mounted by internal/export, not here: it reads
	// memories and conversations as well as documents, and this package has no
	// business importing its peers to serve one page.
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	h.renderIndex(w, r, http.StatusOK, knowledgepages.NoteForm{})
}

func (h *Handler) createNote(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := knowledgepages.NoteForm{
		Title: strings.TrimSpace(r.PostFormValue("title")),
		Body:  strings.TrimSpace(r.PostFormValue("body")),
	}

	if _, err := h.svc.CreateNote(r.Context(), user.ID, form.Title, form.Body); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.renderIndex(w, r, http.StatusUnprocessableEntity, form)
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/knowledge", http.StatusSeeOther)
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
		h.renderIndex(w, r, http.StatusUnprocessableEntity, knowledgepages.NoteForm{
			Errors: map[string]string{"file": "That upload could not be read."},
		})
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, header, err := r.FormFile("file")
	if err != nil {
		h.renderIndex(w, r, http.StatusUnprocessableEntity, knowledgepages.NoteForm{
			Errors: map[string]string{"file": "Choose a file first."},
		})
		return
	}
	defer func() { _ = file.Close() }()

	mime := header.Header.Get("Content-Type")
	if mime == "" {
		mime = "text/plain"
	}

	if _, err := h.svc.Upload(r.Context(), user.ID, header.Filename, mime, file); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			h.renderIndex(w, r, http.StatusUnprocessableEntity, knowledgepages.NoteForm{
				Errors: fieldErrs.Messages(),
			})
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/knowledge", http.StatusSeeOther)
}

// reindex queues a rebuild rather than running one.
//
// A person with a large library would otherwise sit on a POST for as long as it
// takes to re-read everything, and a browser that gave up half way would leave
// them unable to tell whether it had finished.
func (h *Handler) reindex(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if err := h.svc.Reindex(r.Context(), user.ID); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/knowledge", http.StatusSeeOther)
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := h.svc.Delete(r.Context(), id, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/knowledge", http.StatusSeeOther)
}

func (h *Handler) renderIndex(w http.ResponseWriter, r *http.Request, status int, form knowledgepages.NoteForm) {
	user := auth.MustUser(r.Context())

	view := knowledgepages.View{}

	docs, err := h.svc.List(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view.Documents = docs

	if counts, err := h.svc.Counts(r.Context(), user.ID); err == nil {
		view.Counts = counts
	}
	if run, err := h.svc.LatestRun(r.Context(), user.ID); err == nil {
		view.LastRun, view.HasRun = run, true
	}

	render(w, r, status, knowledgepages.IndexPage(user, view, form))
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("knowledge request failed", slog.Any("error", err))
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
