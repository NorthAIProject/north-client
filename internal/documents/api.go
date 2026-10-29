package documents

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/search"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the knowledge library for native clients: the documents and notes
// the coach can search, the search itself, and uploads from the phone. The
// same service and the same quotas as the web /knowledge page.
type API struct {
	svc    *Service
	quotas *quota.Service
}

// NewAPI builds the routes. Routes go behind auth.RequireBearer with the API's
// JSON body cap; UploadRoutes go in the upload group, whose cap fits a file.
func NewAPI(svc *Service, quotas *quota.Service) *API { return &API{svc: svc, quotas: quotas} }

func (a *API) Routes(r chi.Router) {
	r.Get("/knowledge", a.list)
	r.Get("/knowledge/search", a.search)
	r.Get("/knowledge/passages", a.passages)
	r.With(a.quotas.GuardJSON(quota.DocumentUpload)).Post("/knowledge/notes", a.createNote)
	r.With(a.quotas.GuardJSON(quota.DocumentReindex)).Post("/knowledge/reindex", a.reindexAll)
	r.With(a.quotas.GuardJSON(quota.DocumentReindex)).Post("/knowledge/retry-embeddings", a.retryEmbeddings)
	r.Get("/knowledge/{documentID}", a.show)
	r.With(a.quotas.GuardJSON(quota.DocumentReindex)).Post("/knowledge/{documentID}/reindex", a.reindex)
	r.Delete("/knowledge/{documentID}", a.destroy)
}

func (a *API) UploadRoutes(r chi.Router) {
	r.With(a.quotas.GuardJSON(quota.DocumentUpload)).Post("/knowledge/uploads", a.upload)
}

type DocumentView struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	// Kind is upload or note.
	Kind     string `json:"kind"`
	MIME     string `json:"mime"`
	ByteSize int64  `json:"byteSize"`
	// Status is pending (being read), ready or failed.
	Status     string     `json:"status"`
	ParseError string     `json:"parseError,omitempty"`
	IndexedAt  *time.Time `json:"indexedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	// LineCount is how many lines the reader numbered; 0 before the first read.
	LineCount int `json:"lineCount"`
	// Stale is a ready document whose text changed after it was last read.
	Stale bool `json:"stale"`
}

type KnowledgeCounts struct {
	Ready   int `json:"ready"`
	Pending int `json:"pending"`
	Failed  int `json:"failed"`
	Stale   int `json:"stale"`
	// Chunks is how many passages the coach can search.
	Chunks int `json:"chunks"`
}

type KnowledgeList struct {
	Documents []DocumentView  `json:"documents"`
	Counts    KnowledgeCounts `json:"counts"`
	// Problems are the documents that need something done to them, worst
	// first: kind is failed, empty, stale, old-reader or stuck (an open set).
	Problems []KnowledgeProblem `json:"problems"`
	// EmbeddingGap is how many passages are searchable by word but not yet by
	// meaning. Zero when no embedding provider is configured.
	EmbeddingGap int `json:"embeddingGap"`
	// LastRun is the most recent indexing pass; absent when there never was one.
	LastRun *IndexRunView `json:"lastRun,omitempty"`
}

type KnowledgeProblem struct {
	DocumentID uuid.UUID `json:"documentId"`
	Title      string    `json:"title"`
	Kind       string    `json:"kind"`
	Detail     string    `json:"detail"`
}

// IndexRunView is one indexing pass. CompletedAt is absent while it runs.
type IndexRunView struct {
	StartedAt   time.Time  `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	Seen        int        `json:"seen"`
	Added       int        `json:"added"`
	Updated     int        `json:"updated"`
	Unchanged   int        `json:"unchanged"`
	Failed      int        `json:"failed"`
}

type DocumentDetail struct {
	DocumentView
	// Text is the document as the coach reads it, line by line.
	Text string `json:"text"`
	// Headings are the section markers, each with its one-based line in Text.
	Headings []HeadingView `json:"headings"`
}

type HeadingView struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
	Line  int    `json:"line"`
}

type SearchHit struct {
	DocumentID  uuid.UUID `json:"documentId"`
	Title       string    `json:"title"`
	HeadingPath []string  `json:"headingPath"`
	// Snippet is the excerpt as plain text; Segments is the same excerpt
	// split into runs, with the ones the search matched marked.
	Snippet   string           `json:"snippet"`
	Segments  []SnippetSegment `json:"segments"`
	StartLine int              `json:"startLine"`
	EndLine   int              `json:"endLine"`
}

type SnippetSegment struct {
	Text    string `json:"text"`
	Matched bool   `json:"matched"`
}

type SearchResults struct {
	Hits []SearchHit `json:"hits"`
	// HasMore says a further page exists past offset+limit.
	HasMore bool `json:"hasMore"`
}

type NoteRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUser(r.Context()).ID
	docs, err := a.svc.List(r.Context(), userID)
	if err != nil {
		httpx.Error(w, err, "Knowledge could not be loaded.")
		return
	}
	counts, err := a.svc.Counts(r.Context(), userID)
	if err != nil {
		httpx.Error(w, err, "Knowledge could not be loaded.")
		return
	}
	out := KnowledgeList{
		Documents: make([]DocumentView, 0, len(docs)),
		Counts:    KnowledgeCounts{Ready: counts.Ready, Pending: counts.Pending, Failed: counts.Failed, Stale: counts.Stale},
	}
	out.Counts.Chunks = counts.Chunks
	for _, d := range docs {
		out.Documents = append(out.Documents, project(d))
	}

	// The health panel is best effort, as on the web page: a failed lookup
	// shows the library without it rather than no library.
	out.Problems = []KnowledgeProblem{}
	if problems, err := a.svc.Attention(r.Context(), userID); err == nil {
		for _, p := range problems {
			out.Problems = append(out.Problems, KnowledgeProblem{DocumentID: p.DocumentID, Title: p.Title, Kind: p.Kind, Detail: p.Detail})
		}
	}
	out.EmbeddingGap = a.svc.EmbeddingGap(r.Context(), userID)
	if run, err := a.svc.LatestRun(r.Context(), userID); err == nil {
		out.LastRun = &IndexRunView{
			StartedAt: run.StartedAt, CompletedAt: run.CompletedAt, Seen: run.Seen, Added: run.Added,
			Updated: run.Updated, Unchanged: run.Unchanged, Failed: run.Failed,
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	out := SearchResults{Hits: []SearchHit{}}
	if query != "" {
		hits, hasMore, err := a.svc.SearchPage(r.Context(), auth.MustUser(r.Context()).ID, query, limit, offset)
		if err != nil {
			// A query of nothing but punctuation is no query, as on the web page.
			if apperr.Is(err, search.ErrEmptyTerm) {
				httpx.WriteJSON(w, http.StatusOK, out)
				return
			}
			httpx.Error(w, err, "Search did not work just now.")
			return
		}
		out.HasMore = hasMore
		for _, h := range hits {
			out.Hits = append(out.Hits, projectHit(h))
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// passages resolves the citations under one reply. Refs arrive as the message
// stored them (chunk:<id>, memory:<id>); only chunk refs resolve, and only the
// caller's own.
func (a *API) passages(w http.ResponseWriter, r *http.Request) {
	msg := conversations.Message{EvidenceRefs: r.URL.Query()["ref"]}
	out := SearchResults{Hits: []SearchHit{}}
	if ids := msg.ChunkIDs(); len(ids) > 0 {
		hits, err := a.svc.Passages(r.Context(), auth.MustUser(r.Context()).ID, ids)
		if err != nil {
			httpx.Error(w, err, "Sources could not be loaded.")
			return
		}
		for _, h := range hits {
			out.Hits = append(out.Hits, projectHit(h))
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) retryEmbeddings(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.RetryEmbeddings(r.Context(), auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "Retrying could not be started.")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) createNote(w http.ResponseWriter, r *http.Request) {
	var req NoteRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 1 << 20}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	doc, err := a.svc.CreateNote(r.Context(), auth.MustUser(r.Context()).ID, strings.TrimSpace(req.Title), strings.TrimSpace(req.Body))
	if err != nil {
		httpx.Error(w, err, "The note could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, project(doc))
}

// upload takes one file as multipart/form-data, field "file", as the web form
// sends it.
func (a *API) upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
		httpx.Error(w, apperr.FieldErrors{}.Add("file", "That upload could not be read."), "That upload could not be read.")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, apperr.FieldErrors{}.Add("file", "Choose a file first."), "Choose a file first.")
		return
	}
	defer func() { _ = file.Close() }()

	mime := header.Header.Get("Content-Type")
	if mime == "" {
		mime = "text/plain"
	}
	doc, err := a.svc.Upload(r.Context(), auth.MustUser(r.Context()).ID, header.Filename, mime, file)
	if err != nil {
		httpx.Error(w, err, "The file could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, project(doc))
}

func (a *API) show(w http.ResponseWriter, r *http.Request) {
	id, ok := documentID(w, r)
	if !ok {
		return
	}
	doc, content, err := a.svc.Content(r.Context(), id, auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "The document could not be loaded.")
		return
	}
	headings := make([]HeadingView, 0, len(content.Headings))
	for _, h := range content.Headings {
		headings = append(headings, HeadingView{Level: h.Level, Text: h.Text, Line: h.Line})
	}
	httpx.WriteJSON(w, http.StatusOK, DocumentDetail{DocumentView: project(doc), Text: strings.Join(content.Lines, "\n"), Headings: headings})
}

func (a *API) reindexAll(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.Reindex(r.Context(), auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "Reindexing could not be started.")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) reindex(w http.ResponseWriter, r *http.Request) {
	id, ok := documentID(w, r)
	if !ok {
		return
	}
	if err := a.svc.ReindexDocument(r.Context(), id, auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "Reindexing could not be started.")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := documentID(w, r)
	if !ok {
		return
	}
	if err := a.svc.Delete(r.Context(), id, auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "The document could not be deleted.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// projectHit resolves the raw snippet's ts_headline markers, which are control
// characters, into plain text and matched segments, as the web page does.
func projectHit(h Hit) SearchHit {
	path := h.HeadingPath
	if path == nil {
		path = []string{}
	}
	hit := SearchHit{DocumentID: h.DocumentID, Title: h.Title, HeadingPath: path, Segments: []SnippetSegment{}, StartLine: h.StartLine, EndLine: h.EndLine}
	var plain strings.Builder
	for _, seg := range h.Segments() {
		plain.WriteString(seg.Text)
		hit.Segments = append(hit.Segments, SnippetSegment{Text: seg.Text, Matched: seg.Matched})
	}
	hit.Snippet = plain.String()
	if len(hit.Segments) == 0 {
		// A vector-only match never went through ts_headline: the web page
		// shows the start of the passage instead, and so does the snippet.
		hit.Snippet = excerpt(h.Content)
	}
	return hit
}

func project(d Document) DocumentView {
	return DocumentView{
		ID: d.ID, Title: d.Title, Kind: d.SourceKind, MIME: d.MIME, ByteSize: d.ByteSize,
		Status: d.Status, ParseError: d.ParseError, IndexedAt: d.IndexedAt, CreatedAt: d.CreatedAt,
		LineCount: d.LineCount, Stale: d.IsStale(),
	}
}

func documentID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "documentID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return uuid.Nil, false
	}
	return id, true
}

// excerpt is the start of a passage, for a hit with no highlighted snippet.
// Cut on a rune boundary so a multi-byte character is never split.
func excerpt(content string) string {
	const max = 240
	if len(content) <= max {
		return content
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}
	return content[:cut] + "…"
}
