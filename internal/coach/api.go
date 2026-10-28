package coach

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/quota"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/watches"
)

// API is the coach for native clients: the same service the web chat and
// Telegram reach, with JSON in and typed JSON event frames out.
//
// The one shape that differs from the web is sending a message. The web posts
// the text, then opens a GET event stream for the reply. Here a single POST
// carries the text in its body and answers with the stream, so user content
// never travels in a URL and one request is one turn.
type API struct {
	svc    *Service
	quotas *quota.Service
	media  chatMedia

	attachmentURL attachmentURL
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service, quotas *quota.Service, media chatMedia) *API {
	return &API{svc: svc, quotas: quotas, media: media}
}

// Routes mounts routes relative to /api/v1.
func (a *API) Routes(r chi.Router) {
	r.Get("/conversations", a.list)
	r.Post("/conversations", a.start)
	r.Get("/conversations/{id}", a.show)
	r.Delete("/conversations/{id}", a.delete)
	r.Post("/conversations/{id}/reply", a.reply)
	r.Post("/conversations/{id}/resume", a.resume)
	r.Post("/conversations/{id}/tools/{messageID}", a.decide)
	r.Put("/conversations/{id}/messages/{messageID}/helpful", a.rate)
	r.Get("/conversations/stats", a.stats)
	r.Get("/conversations/{id}/attachments/{mediaID}", a.attachment)
}

// attachmentURL resolves a stored chat attachment, owned by userID, to a URL
// the browser can load it from. Supplied by cmd/web from the media service,
// so this package still does not import internal/media.
type attachmentURL func(ctx context.Context, mediaID, userID uuid.UUID) (string, error)

// WithAttachmentURLs turns on GET .../attachments/{mediaID}, which the web
// transcript uses for a photo's <img>.
func (a *API) WithAttachmentURLs(resolve attachmentURL) *API {
	a.attachmentURL = resolve
	return a
}

// UploadRoutes are the routes that take a file; mount them in the upload
// group, whose body cap fits one.
func (a *API) UploadRoutes(r chi.Router) {
	r.Post("/conversations/{id}/attachments", a.uploadAttachment)
}

// ConversationSummary is a row in the conversation list.
type ConversationSummary struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Kind      string    `json:"kind"`
	Ended     bool      `json:"ended"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ConversationList struct {
	Conversations []ConversationSummary `json:"conversations"`
}

// ConversationDetail is one thread with everything a client renders.
type ConversationDetail struct {
	Conversation ConversationSummary `json:"conversation"`
	// Summary is a finished reflection's write-up.
	Summary  string    `json:"summary,omitempty"`
	Messages []Message `json:"messages"`
	// PendingApproval is set when the coach stopped to ask before changing
	// something; answer it with POST .../tools/{messageId}.
	PendingApproval *Approval `json:"pendingApproval,omitempty"`
	// AwaitingResume means an approval was answered and the coach has not
	// replied yet; POST .../resume streams the rest.
	AwaitingResume bool `json:"awaitingResume"`
}

// Message is one visible turn. Tool plumbing turns are left out: they are
// the coach talking to itself, and the web does not show them either.
type Message struct {
	ID          uuid.UUID    `json:"id"`
	Role        string       `json:"role"`
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments"`
	// Exercises are catalog slugs the coach looked up for this reply, for
	// rendering exercise cards beside it.
	Exercises []string  `json:"exercises"`
	Helpful   *bool     `json:"helpful,omitempty"`
	CreatedAt time.Time `json:"createdAt"`

	// ToolTurn marks a coach turn that called tools. Its text is part of what
	// the model sees; the web transcript leaves the whole turn out.
	ToolTurn bool `json:"toolTurn,omitempty"`
	// Proactive is a turn nobody asked for (briefing, standing task,
	// check-in); SourceLabel names what sent it, empty for "proactive".
	Proactive   bool   `json:"proactive,omitempty"`
	SourceLabel string `json:"sourceLabel,omitempty"`
	// EvidenceRefs are the stored refs the reply drew on, to pass to
	// GET /knowledge/passages; SourceCount is how many are document passages.
	EvidenceRefs []string `json:"evidenceRefs,omitempty"`
	SourceCount  int      `json:"sourceCount,omitempty"`
}

type Attachment struct {
	MediaID  uuid.UUID `json:"mediaId"`
	Kind     string    `json:"kind"`
	MIMEType string    `json:"mimeType"`
	Name     string    `json:"name"`
}

// Approval is a write the coach wants to make, waiting for a yes or no.
type Approval struct {
	MessageID uuid.UUID      `json:"messageId"`
	Calls     []ApprovalCall `json:"calls"`
}

type ApprovalCall struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	// Watch is set when the call is create_watch and its arguments parse:
	// a client shows it as a standing task rather than as a tool call.
	Watch *WatchProposal `json:"watch,omitempty"`
}

// WatchProposal is a standing task the coach proposes to run.
type WatchProposal struct {
	Title     string `json:"title"`
	Condition string `json:"condition"`
	Spec      string `json:"spec"`
	// Cadence is "daily" or "weekly" (an open string).
	Cadence string `json:"cadence"`
	// Weekday is 0 (Sunday) to 6, read only for a weekly watch.
	Weekday int `json:"weekday"`
	// Clock is the local time of day it runs, "8:00", "18:30".
	Clock string `json:"clock"`
}

// CoachStats is the coach page's two counters.
type CoachStats struct {
	ThreadCount      int `json:"threadCount"`
	MessagesThisWeek int `json:"messagesThisWeek"`
}

type StartRequest struct {
	// Kind is "chat" (default) or "reflection".
	Kind string `json:"kind"`
}

type ReplyRequest struct {
	Text string `json:"text"`
	// MediaID is a photo or document already uploaded to this account, by
	// POST /conversations/{id}/attachments.
	MediaID *uuid.UUID `json:"mediaId,omitempty"`
	// Begin asks a new reflection to open with the coach's first question
	// instead of answering a message.
	Begin bool `json:"begin,omitempty"`
}

type DecisionRequest struct {
	Approve bool `json:"approve"`
}

type HelpfulRequest struct {
	// Helpful is true, false, or null to clear the answer.
	Helpful *bool `json:"helpful"`
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	list, err := a.svc.Conversations().List(r.Context(), user.ID, 50)
	if err != nil {
		httpx.Error(w, err, "Conversations could not be loaded.")
		return
	}
	out := ConversationList{Conversations: make([]ConversationSummary, 0, len(list))}
	for _, c := range list {
		out.Conversations = append(out.Conversations, projectSummary(c))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) start(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	var req StartRequest
	if r.ContentLength != 0 {
		if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
			httpx.Error(w, err, "The request body must be a valid conversation request.")
			return
		}
	}

	var (
		conversation conversations.Conversation
		err          error
	)
	switch strings.TrimSpace(req.Kind) {
	case "", conversations.KindChat:
		conversation, err = a.svc.StartConversation(r.Context(), user.ID)
	case conversations.KindReflection:
		conversation, err = a.svc.StartReflection(r.Context(), user.ID)
	default:
		httpx.Error(w, apperr.FieldErrors{}.Add("kind", "Kind must be chat or reflection."), "Unknown conversation kind.")
		return
	}
	if err != nil {
		httpx.Error(w, err, "The conversation could not be started.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, projectSummary(conversation))
}

func (a *API) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	conversation, err := a.svc.Conversations().Get(r.Context(), id, user.ID)
	if err != nil {
		httpx.Error(w, err, "That conversation was not found.")
		return
	}
	history, err := a.svc.Conversations().History(r.Context(), conversation.ID)
	if err != nil {
		httpx.Error(w, err, "The conversation could not be loaded.")
		return
	}
	pending, waiting, err := a.svc.PendingApproval(r.Context(), user, conversation.ID)
	if err != nil {
		httpx.Error(w, err, "The conversation could not be loaded.")
		return
	}

	detail := ConversationDetail{
		Conversation: projectSummary(conversation),
		Summary:      conversation.Summary,
		Messages:     projectMessages(history),
	}
	if waiting {
		detail.PendingApproval = projectApproval(pending)
	}
	// Same rule the web page uses: a trailing tool result means the model
	// was handed an answer and has not replied yet.
	detail.AwaitingResume = !waiting && len(history) > 0 && len(history[len(history)-1].ToolResults) > 0
	httpx.WriteJSON(w, http.StatusOK, detail)
}

// stats counts threads and this week's messages, for the coach page's strip.
func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	list, err := a.svc.Conversations().List(r.Context(), user.ID, 50)
	if err != nil {
		httpx.Error(w, err, "Conversations could not be loaded.")
		return
	}
	stats, err := buildCoachStats(r.Context(), a.svc.Conversations(), list, time.Now())
	if err != nil {
		httpx.Error(w, err, "Conversations could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CoachStats{ThreadCount: stats.ThreadCount, MessagesThisWeek: stats.MessagesThisWeek})
}

// attachment redirects to a file a turn carried, for this account only.
func (a *API) attachment(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if _, ok := pathID(w, r, "id"); !ok {
		return
	}
	mediaID, ok := pathID(w, r, "mediaID")
	if !ok {
		return
	}
	if a.attachmentURL == nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	url, err := a.attachmentURL(r.Context(), mediaID, user.ID)
	if err != nil {
		httpx.Error(w, err, "That file is not available.")
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := a.svc.Conversations().Delete(r.Context(), id, user.ID); err != nil {
		httpx.Error(w, err, "The conversation could not be deleted.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reply stores the person's message and streams the coach's answer.
//
// Everything that can be refused cheaply (bad JSON, empty turn, unknown or
// ended conversation, spent quota) is refused before the stream starts, as a
// normal JSON error with a status code. Once the stream is open, failures
// arrive as `error` frames.
func (a *API) reply(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}

	var req ReplyRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 64 << 10}); err != nil {
		httpx.Error(w, err, "The request body must be a valid reply request.")
		return
	}

	conversation, err := a.svc.Conversations().Get(r.Context(), id, user.ID)
	if err != nil {
		httpx.Error(w, err, "That conversation was not found.")
		return
	}
	if conversation.Ended() {
		httpx.Error(w, apperr.ErrConflict, "This reflection has ended.")
		return
	}

	in := Incoming{Text: strings.TrimSpace(req.Text)}
	if req.MediaID != nil && a.media != nil {
		// Built only from a record this account owns, as on the web.
		attachment, loadErr := a.media.LoadChatAttachment(r.Context(), *req.MediaID, user.ID)
		if loadErr != nil {
			httpx.Error(w, apperr.FieldErrors{}.Add("mediaId", "That file is not available."), "That file is not available.")
			return
		}
		in.Attachments = []conversations.Attachment{attachment}
	}
	if !req.Begin {
		if turnErr := conversations.ValidateTurn(in.Text, len(in.Attachments) > 0); turnErr != nil {
			httpx.Error(w, apperr.FieldErrors{}.Add("text", "Type something first."), "Type something first.")
			return
		}
	}

	// Spent here, where the model is reached, exactly as on the web stream.
	decision, err := a.quotas.Consume(r.Context(), user.ID, string(user.Tier), quota.CoachMessage)
	if err == nil && !decision.Allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(decision.RetryAfter.Seconds())+1))
		httpx.WriteJSON(w, http.StatusTooManyRequests, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: quotaMessage(decision)}})
		return
	}

	var stream <-chan ai.StreamChunk
	if req.Begin {
		stream, err = a.svc.BeginReflection(r.Context(), user, conversation.ID)
	} else {
		stream, err = a.svc.SendIncoming(r.Context(), user, conversation.ID, in)
	}
	if err != nil {
		httpx.Error(w, err, friendly(err))
		return
	}
	a.relay(w, r, conversation.ID, stream)
}

// uploadAttachment stores a photo or document for the next reply to carry, and
// answers with its reference: send it as mediaId on replyInConversation.
//
// It spends no quota. Nothing reaches a model here; the reply that carries the
// file is what costs a message.
func (a *API) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if _, err := a.svc.Conversations().Get(r.Context(), id, user.ID); err != nil {
		httpx.Error(w, err, "That conversation was not found.")
		return
	}
	if a.media == nil {
		httpx.Error(w, apperr.ErrUnavailable, "Attachments are not available right now.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxChatUpload)
	if err := r.ParseMultipartForm(maxChatUpload); err != nil {
		var tooBig *http.MaxBytesError
		if apperr.As(err, &tooBig) {
			httpx.Error(w, httpx.ErrTooLarge, "That file is larger than 8 MB.")
			return
		}
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

	stored, err := a.media.StoreChatAttachment(r.Context(), user.ID, header.Filename, header.Size, file)
	if err != nil {
		var fields apperr.FieldErrors
		if apperr.As(err, &fields) && len(fields) > 0 {
			httpx.Error(w, err, fields[0].Message)
			return
		}
		httpx.Error(w, err, "That file could not be stored.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, projectAttachment(stored))
}

// resume streams the rest of a reply whose turn stopped for approval. It spends
// no quota: answering a confirmation is not a second question.
func (a *API) resume(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	stream, err := a.svc.Resume(r.Context(), user, id)
	if err != nil {
		httpx.Error(w, err, friendly(err))
		return
	}
	a.relay(w, r, id, stream)
}

// decide runs or refuses the waiting tool calls. The tools run inside this
// request, so a retried stream can never run a write twice.
func (a *API) decide(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	messageID, ok := pathID(w, r, "messageID")
	if !ok {
		return
	}
	var req DecisionRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 1 << 10}); err != nil {
		httpx.Error(w, err, "The request body must say approve true or false.")
		return
	}
	if err := a.svc.ResolvePending(r.Context(), user, id, messageID, req.Approve); err != nil {
		httpx.Error(w, err, "That request could not be answered.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) rate(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	messageID, ok := pathID(w, r, "messageID")
	if !ok {
		return
	}
	var req HelpfulRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 1 << 10}); err != nil {
		httpx.Error(w, err, "The request body must carry helpful true, false or null.")
		return
	}
	// The update scopes itself to the owner through the message's thread.
	msg, err := a.svc.Conversations().SetMessageHelpful(r.Context(), messageID, user.ID, req.Helpful)
	if err != nil {
		httpx.Error(w, err, "That reply could not be rated.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectMessage(msg))
}

// Event frames on a reply or resume stream. Each is one SSE event whose data
// is a single line of JSON.
//
//	token     {"text": "..."}                    append to the reply
//	error     {"message": "..."}                 the reply failed; show this
//	exercises {"slugs": ["push-up"]}             render these exercise cards
//	approval  Approval                           ask before the coach writes
//	status    {"line": "is checking your goals"} a round of tool calls is running
//	done      {"messageId": "..."}               the reply is stored
const (
	EventToken     = "token"
	EventError     = "error"
	EventExercises = "exercises"
	EventApproval  = "approval"
	EventDone      = "done"
	EventStatus    = "status"
)

// StatusEvent is the header's status line while tools run, already in the
// reader's language.
type StatusEvent struct {
	Line string `json:"line"`
}

type TokenEvent struct {
	Text string `json:"text"`
}

type ErrorEvent struct {
	Message string `json:"message"`
}

type ExercisesEvent struct {
	Slugs []string `json:"slugs"`
}

type DoneEvent struct {
	// MessageID is the stored reply, for rating it. Nil when the turn stopped
	// for approval before any reply was written.
	MessageID *uuid.UUID `json:"messageId,omitempty"`
}

// relay forwards the service's stream as frames, then reports what the caller
// can only learn once it has closed: which exercises the reply drew on, and
// whether it stopped to ask before writing something.
func (a *API) relay(w http.ResponseWriter, r *http.Request, conversationID uuid.UUID, stream <-chan ai.StreamChunk) {
	user := auth.MustUser(r.Context())
	log := middleware.FromContext(r.Context())

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	_ = rc.Flush()

	failed := false
	for chunk := range stream {
		if chunk.Err != nil {
			log.Error("coach stream failed", slog.Any("error", chunk.Err))
			writeEvent(w, rc, EventError, ErrorEvent{Message: friendly(chunk.Err)})
			failed = true
			break
		}
		if len(chunk.ToolCalls) > 0 {
			writeEvent(w, rc, EventStatus, StatusEvent{Line: sseLine(toolStatus(r.Context(), chunk.ToolCalls))})
			continue
		}
		if chunk.Text != "" {
			writeEvent(w, rc, EventToken, TokenEvent{Text: chunk.Text})
		}
	}

	var done DoneEvent
	if !failed {
		if slugs, err := a.svc.LatestExerciseRefs(r.Context(), user, conversationID); err == nil && len(slugs) > 0 {
			writeEvent(w, rc, EventExercises, ExercisesEvent{Slugs: slugs})
		}
		if pending, waiting, err := a.svc.PendingApproval(r.Context(), user, conversationID); err == nil && waiting {
			writeEvent(w, rc, EventApproval, projectApproval(pending))
		} else if history, err := a.svc.Conversations().History(r.Context(), conversationID); err == nil {
			for i := len(history) - 1; i >= 0; i-- {
				if history[i].IsModel() && !history[i].IsToolTurn() {
					done.MessageID = &history[i].ID
					break
				}
			}
		}
	}
	writeEvent(w, rc, EventDone, done)
}

// writeEvent emits one named frame. JSON never contains a raw newline, so the
// data fits on the single line SSE requires.
func writeEvent(w http.ResponseWriter, rc *http.ResponseController, event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(`{}`)
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	_ = rc.Flush()
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return uuid.Nil, false
	}
	return id, true
}

func projectSummary(c conversations.Conversation) ConversationSummary {
	return ConversationSummary{ID: c.ID, Title: c.DisplayTitle(), Kind: c.Kind, Ended: c.Ended(), UpdatedAt: c.UpdatedAt}
}

func projectMessages(history []conversations.Message) []Message {
	out := make([]Message, 0, len(history))
	for _, m := range history {
		if m.IsToolTurn() && strings.TrimSpace(m.Content) == "" {
			continue
		}
		if !m.IsUser() && !m.IsModel() {
			continue
		}
		out = append(out, projectMessage(m))
	}
	return out
}

func projectMessage(m conversations.Message) Message {
	role := "user"
	if m.IsModel() {
		role = "coach"
	}
	attachments := make([]Attachment, 0, len(m.Parts))
	for _, p := range m.Parts {
		attachments = append(attachments, projectAttachment(p))
	}
	return Message{
		ID:          m.ID,
		Role:        role,
		Text:        m.Content,
		Attachments: attachments,
		Exercises:   m.ExerciseSlugs(),
		Helpful:     m.Helpful,
		CreatedAt:   m.CreatedAt,

		ToolTurn:     m.IsToolTurn(),
		Proactive:    m.IsProactive(),
		SourceLabel:  strings.TrimSpace(m.SourceLabel),
		EvidenceRefs: sourceRefs(m),
		SourceCount:  len(m.ChunkIDs()),
	}
}

// sourceRefs is the refs to resolve for a reply's sources disclosure, or none
// when no document passage is among them.
func sourceRefs(m conversations.Message) []string {
	if len(m.ChunkIDs()) == 0 {
		return nil
	}
	return m.EvidenceRefs
}

func projectAttachment(a conversations.Attachment) Attachment {
	return Attachment{MediaID: a.MediaID, Kind: a.Kind, MIMEType: a.MIMEType, Name: a.Name}
}

func projectApproval(p PendingCall) *Approval {
	out := &Approval{MessageID: p.MessageID, Calls: make([]ApprovalCall, 0, len(p.Calls))}
	for _, call := range p.Calls {
		item := ApprovalCall{Name: call.Name, Summary: describeCall(call)}
		if call.Name == watches.ToolName {
			if w, err := watches.ParseProposal(call.Arguments); err == nil {
				item.Watch = &WatchProposal{
					Title: w.Title, Condition: w.Condition, Spec: w.Spec,
					Cadence: string(w.Schedule.Cadence), Weekday: int(w.Schedule.Weekday), Clock: w.Schedule.Clock(),
				}
			}
		}
		out.Calls = append(out.Calls, item)
	}
	return out
}
