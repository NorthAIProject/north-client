package documents

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/quota"
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
	r.With(a.quotas.GuardJSON(quota.DocumentUpload)).Post("/knowledge/notes", a.createNote)
	r.With(a.quotas.GuardJSON(quota.DocumentReindex)).Post("/knowledge/reindex", a.reindexAll)
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
}

type KnowledgeCounts struct {
	Ready   int `json:"ready"`
	Pending int `json:"pending"`
	Failed  int `json:"failed"`
	Stale   int `json:"stale"`
}

type KnowledgeList struct {
	Documents []DocumentView  `json:"documents"`
	Counts    KnowledgeCounts `json:"counts"`
}

type DocumentDetail struct {
	DocumentView
	// Text is the document as the coach reads it, line by line.
	Text string `json:"text"`
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
}

type SnippetSegment struct {
	Text    string `json:"text"`
	Matched bool   `json:"matched"`
}

type SearchResults struct {
	Hits []SearchHit `json:"hits"`
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
	for _, d := range docs {
		out.Documents = append(out.Documents, project(d))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	out := SearchResults{Hits: []SearchHit{}}
	if query != "" {
		hits, err := a.svc.Search(r.Context(), auth.MustUser(r.Context()).ID, query, limit)
		if err != nil {
			httpx.Error(w, err, "Search did not work just now.")
			return
		}
		for _, h := range hits {
			out.Hits = append(out.Hits, projectHit(h))
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
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
	httpx.WriteJSON(w, http.StatusOK, DocumentDetail{DocumentView: project(doc), Text: strings.Join(content.Lines, "\n")})
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
	hit := SearchHit{DocumentID: h.DocumentID, Title: h.Title, HeadingPath: path, Segments: []SnippetSegment{}, StartLine: h.StartLine}
	var plain strings.Builder
	for _, seg := range h.Segments() {
		plain.WriteString(seg.Text)
		hit.Segments = append(hit.Segments, SnippetSegment{Text: seg.Text, Matched: seg.Matched})
	}
	hit.Snippet = plain.String()
	return hit
}

func project(d Document) DocumentView {
	return DocumentView{
		ID: d.ID, Title: d.Title, Kind: d.SourceKind, MIME: d.MIME, ByteSize: d.ByteSize,
		Status: d.Status, ParseError: d.ParseError, IndexedAt: d.IndexedAt, CreatedAt: d.CreatedAt,
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
