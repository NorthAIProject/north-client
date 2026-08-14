package documents

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/search"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/htmx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
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
	r.Post("/knowledge/retry-embeddings", h.retryEmbeddings)

	// Before /knowledge/{id}: chi matches literal segments ahead of parameters,
	// but keeping them adjacent makes the ordering something a reader can see
	// rather than something they have to know.
	r.Get("/knowledge/search", h.search)
	r.Get("/knowledge/passages", h.passages)

	r.Get("/knowledge/{id}", h.show)
	r.Post("/knowledge/{id}/reindex", h.reindexOne)
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

// show renders a document with the lines a citation points at highlighted.
//
// from and to are the passage's own line numbers, carried in the query rather
// than only in the fragment because the server draws the highlight and a
// fragment never reaches it.
func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	from, to := highlightRange(r)

	doc, parsed, err := h.svc.Content(r.Context(), id, user.ID)
	if err != nil {
		// A document whose bytes cannot be read is still a document its owner
		// should be able to see the record of, and the reason it failed is on
		// the row. Rendering the page without a body says more than a 500.
		if apperr.Is(err, apperr.ErrNotFound) {
			h.fail(w, r, err)
			return
		}
		render(w, r, http.StatusOK, knowledgepages.DocumentPage(user, knowledgepages.DocumentView{
			Document:  doc,
			ReadError: err.Error(),
		}))
		return
	}

	render(w, r, http.StatusOK, knowledgepages.DocumentPage(user, knowledgepages.DocumentView{
		Document:  doc,
		Lines:     parsed.Lines,
		Headings:  parsed.Headings,
		FromLine:  from,
		ToLine:    to,
		Highlight: from > 0,
	}))
}

// highlightRange reads the passage bounds off the query string.
//
// Anything unreadable becomes no highlight rather than an error: a hand-edited
// URL should show the document, not a failure.
func highlightRange(r *http.Request) (from, to int) {
	from, _ = strconv.Atoi(r.URL.Query().Get("from"))
	to, _ = strconv.Atoi(r.URL.Query().Get("to"))
	if from < 1 {
		return 0, 0
	}
	if to < from {
		to = from
	}
	return from, to
}

// search renders the dedicated search page, an HTMX panel, or the index preview.
func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	if htmx.IsRequest(r) && r.Header.Get("HX-Target") == "knowledge-search-results" {
		h.renderInlineSearch(w, r, user, query)
		return
	}

	limit, offset, appendMode := searchPageParams(r)

	if query == "" {
		h.renderSearch(w, r, user, knowledgepages.SearchView{Limit: limit}, appendMode)
		return
	}

	hits, hasMore, err := h.svc.SearchPage(r.Context(), user.ID, query, limit, offset)
	if err != nil {
		if apperr.Is(err, search.ErrEmptyTerm) {
			h.renderSearch(w, r, user, knowledgepages.SearchView{Query: query, Limit: limit}, appendMode)
			return
		}
		h.fail(w, r, err)
		return
	}

	view := knowledgepages.SearchView{
		Query:   query,
		Hits:    hits,
		Offset:  offset,
		Limit:   limit,
		HasMore: hasMore,
		Append:  appendMode,
	}
	h.renderSearch(w, r, user, view, appendMode)
}

func (h *Handler) renderInlineSearch(w http.ResponseWriter, r *http.Request, user users.User, query string) {
	hits, hasMore, err := h.svc.SearchPage(r.Context(), user.ID, query, searchLimit, 0)
	if err != nil {
		if apperr.Is(err, search.ErrEmptyTerm) {
			render(w, r, http.StatusOK, knowledgepages.SearchResults(query, nil, false))
			return
		}
		h.fail(w, r, err)
		return
	}
	render(w, r, http.StatusOK, knowledgepages.SearchResults(query, hits, hasMore))
}

func searchPageParams(r *http.Request) (limit, offset int, appendMode bool) {
	limit = searchPageDefault
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v >= 0 {
		offset = v
	}
	appendMode = r.URL.Query().Get("append") == "1"
	return limit, offset, appendMode
}

const searchPageDefault = 20

func (h *Handler) renderSearch(w http.ResponseWriter, r *http.Request, user users.User, view knowledgepages.SearchView, appendMode bool) {
	if htmx.IsRequest(r) {
		if appendMode {
			render(w, r, http.StatusOK, knowledgepages.SearchAppendRows(view))
			return
		}
		render(w, r, http.StatusOK, knowledgepages.SearchPanel(view))
		return
	}
	render(w, r, http.StatusOK, knowledgepages.SearchPage(user, view))
}

func (h *Handler) retryEmbeddings(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if err := h.svc.RetryEmbeddings(r.Context(), user.ID); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/knowledge", http.StatusSeeOther)
}

// passages resolves the citations under one reply.
//
// Scoped to the session user by the query, so a ref copied out of somebody
// else's conversation resolves to nothing.
func (h *Handler) passages(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	// The refs arrive as the message stored them, so the same filter the
	// template used decides what is resolvable here.
	msg := conversations.Message{EvidenceRefs: r.URL.Query()["ref"]}

	hits, err := h.svc.Passages(r.Context(), user.ID, msg.ChunkIDs())
	if err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusOK, knowledgepages.Passages(hits))
}

// reindexOne queues a rebuild of a single document; see reindex.
func (h *Handler) reindexOne(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := h.svc.ReindexDocument(r.Context(), id, user.ID); err != nil {
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

	if counts, countsErr := h.svc.Counts(r.Context(), user.ID); countsErr == nil {
		view.Counts = counts
	}
	if problems, problemsErr := h.svc.Attention(r.Context(), user.ID); problemsErr == nil {
		view.Problems = problems
	}
	view.EmbeddingGap = h.svc.EmbeddingGap(r.Context(), user.ID)
	if run, runErr := h.svc.LatestRun(r.Context(), user.ID); runErr == nil {
		view.LastRun, view.HasRun = run, true
	}

	inst, err := buildInstruments(view.Counts)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view.Instruments = inst

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
