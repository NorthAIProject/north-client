package documents

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/search"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// maxMultipartMemory bounds what an upload buffers in RAM before spilling to a
// temporary file. The service enforces the real size limit.
const maxMultipartMemory = 4 << 20 // 4 MiB

// API is the knowledge library for native clients: the documents and notes
// the coach can search, the search itself, and uploads from the phone. The
// same service and the same quotas as the web /knowledge page.
type API struct {
	svc    *Service
	quotas *quota.Service
}

// NewAPI builds the routes. Register goes behind auth.Bearer with the API's
// JSON body cap; RegisterUploads goes in the upload group, whose cap fits a file.
func NewAPI(svc *Service, quotas *quota.Service) *API { return &API{svc: svc, quotas: quotas} }

func (a *API) Register(g *echo.Group) {
	g.GET("/knowledge", a.list)
	g.GET("/knowledge/search", a.search)
	g.GET("/knowledge/passages", a.passages)
	g.POST("/knowledge/notes", a.createNote, a.quotas.GuardAPI(quota.DocumentUpload))
	g.POST("/knowledge/reindex", a.reindexAll, a.quotas.GuardAPI(quota.DocumentReindex))
	g.POST("/knowledge/retry-embeddings", a.retryEmbeddings, a.quotas.GuardAPI(quota.DocumentReindex))
	g.GET("/knowledge/:documentID", a.show)
	g.POST("/knowledge/:documentID/reindex", a.reindex, a.quotas.GuardAPI(quota.DocumentReindex))
	g.DELETE("/knowledge/:documentID", a.destroy)
}

func (a *API) RegisterUploads(g *echo.Group) {
	g.POST("/knowledge/uploads", a.upload, a.quotas.GuardAPI(quota.DocumentUpload))
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

func (a *API) list(c *echo.Context) error {
	ctx := c.Request().Context()
	userID := auth.MustUser(ctx).ID
	docs, err := a.svc.List(ctx, userID)
	if err != nil {
		return httpx.Fail(err, "Knowledge could not be loaded.")
	}
	counts, err := a.svc.Counts(ctx, userID)
	if err != nil {
		return httpx.Fail(err, "Knowledge could not be loaded.")
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
	if problems, err := a.svc.Attention(ctx, userID); err == nil {
		for _, p := range problems {
			out.Problems = append(out.Problems, KnowledgeProblem{DocumentID: p.DocumentID, Title: p.Title, Kind: p.Kind, Detail: p.Detail})
		}
	}
	out.EmbeddingGap = a.svc.EmbeddingGap(ctx, userID)
	if run, err := a.svc.LatestRun(ctx, userID); err == nil {
		out.LastRun = &IndexRunView{
			StartedAt: run.StartedAt, CompletedAt: run.CompletedAt, Seen: run.Seen, Added: run.Added,
			Updated: run.Updated, Unchanged: run.Unchanged, Failed: run.Failed,
		}
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) search(c *echo.Context) error {
	query := strings.TrimSpace(c.QueryParam("q"))
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	offset, _ := strconv.Atoi(c.QueryParam("offset"))
	if offset < 0 {
		offset = 0
	}
	out := SearchResults{Hits: []SearchHit{}}
	if query != "" {
		ctx := c.Request().Context()
		hits, hasMore, err := a.svc.SearchPage(ctx, auth.MustUser(ctx).ID, query, limit, offset)
		if err != nil {
			// A query of nothing but punctuation is no query, as on the web page.
			if apperr.Is(err, search.ErrEmptyTerm) {
				return c.JSON(http.StatusOK, out)
			}
			return httpx.Fail(err, "Search did not work just now.")
		}
		out.HasMore = hasMore
		for _, h := range hits {
			out.Hits = append(out.Hits, projectHit(h))
		}
	}
	return c.JSON(http.StatusOK, out)
}

// passages resolves the citations under one reply. Refs arrive as the message
// stored them (chunk:<id>, memory:<id>); only chunk refs resolve, and only the
// caller's own.
func (a *API) passages(c *echo.Context) error {
	msg := conversations.Message{EvidenceRefs: c.QueryParams()["ref"]}
	out := SearchResults{Hits: []SearchHit{}}
	if ids := msg.ChunkIDs(); len(ids) > 0 {
		ctx := c.Request().Context()
		hits, err := a.svc.Passages(ctx, auth.MustUser(ctx).ID, ids)
		if err != nil {
			return httpx.Fail(err, "Sources could not be loaded.")
		}
		for _, h := range hits {
			out.Hits = append(out.Hits, projectHit(h))
		}
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) retryEmbeddings(c *echo.Context) error {
	ctx := c.Request().Context()
	if err := a.svc.RetryEmbeddings(ctx, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "Retrying could not be started.")
	}
	return c.NoContent(http.StatusAccepted)
}

func (a *API) createNote(c *echo.Context) error {
	var req NoteRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 1 << 20}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	doc, err := a.svc.CreateNote(ctx, auth.MustUser(ctx).ID, strings.TrimSpace(req.Title), strings.TrimSpace(req.Body))
	if err != nil {
		return httpx.Fail(err, "The note could not be saved.")
	}
	return c.JSON(http.StatusCreated, project(doc))
}

// upload takes one file as multipart/form-data, field "file", as the web form
// sends it.
func (a *API) upload(c *echo.Context) error {
	r := c.Request()
	if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
		return httpx.Fail(apperr.FieldErrors{}.Add("file", "That upload could not be read."), "That upload could not be read.")
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, header, err := r.FormFile("file")
	if err != nil {
		return httpx.Fail(apperr.FieldErrors{}.Add("file", "Choose a file first."), "Choose a file first.")
	}
	defer func() { _ = file.Close() }()

	mime := header.Header.Get("Content-Type")
	if mime == "" {
		mime = "text/plain"
	}
	ctx := r.Context()
	doc, err := a.svc.Upload(ctx, auth.MustUser(ctx).ID, header.Filename, mime, file)
	if err != nil {
		return httpx.Fail(err, "The file could not be saved.")
	}
	return c.JSON(http.StatusCreated, project(doc))
}

func (a *API) show(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "documentID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	doc, content, err := a.svc.Content(ctx, id, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "The document could not be loaded.")
	}
	headings := make([]HeadingView, 0, len(content.Headings))
	for _, h := range content.Headings {
		headings = append(headings, HeadingView{Level: h.Level, Text: h.Text, Line: h.Line})
	}
	return c.JSON(http.StatusOK, DocumentDetail{DocumentView: project(doc), Text: strings.Join(content.Lines, "\n"), Headings: headings})
}

func (a *API) reindexAll(c *echo.Context) error {
	ctx := c.Request().Context()
	if err := a.svc.Reindex(ctx, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "Reindexing could not be started.")
	}
	return c.NoContent(http.StatusAccepted)
}

func (a *API) reindex(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "documentID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.ReindexDocument(ctx, id, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "Reindexing could not be started.")
	}
	return c.NoContent(http.StatusAccepted)
}

func (a *API) destroy(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "documentID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.Delete(ctx, id, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "The document could not be deleted.")
	}
	return c.NoContent(http.StatusNoContent)
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
