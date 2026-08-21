package coach

import (
	"context"
	"fmt"
	"io"
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
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	chatpages "github.com/NorthAIProject/north-client/web/chat"
)

type Handler struct {
	svc    *Service
	quotas *quota.Service
	images imageStore
}

// ChatImage is the handful of fields the composer needs from a stored photo.
// Named here so this package does not import internal/media, which already
// implements a ContextSource and would close a cycle.
type ChatImage struct {
	ID           uuid.UUID
	Kind         string
	MIMEType     string
	OriginalName string
}

// imageStore is the slice of media this handler needs: store a photo, get an id.
type imageStore interface {
	StoreChatImage(ctx context.Context, userID uuid.UUID, filename string, size int64, body io.Reader) (ChatImage, error)
	LoadChatImage(ctx context.Context, id, userID uuid.UUID) (ChatImage, error)
}

// maxChatUpload is slightly above an 8 MB photo so a just-over file is
// rejected by the media service, not by a bare multipart parse.
const maxChatUpload = (8 << 20) + (1 << 20)

func NewHandler(svc *Service, quotas *quota.Service) *Handler {
	return &Handler{svc: svc, quotas: quotas}
}

func (h *Handler) WithImages(store imageStore) *Handler {
	h.images = store
	return h
}

// Routes mounts the chat endpoints. Must be mounted behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/chat", h.index)
	r.Post("/chat", h.startConversation)
	r.Get("/chat/{id}", h.show)
	r.Post("/chat/{id}/messages", h.sendMessage)
	r.Get("/chat/{id}/stream", h.stream)

	// Resuming is a GET because it is an event stream, and it spends no quota:
	// the person asked one question, and answering a confirmation is not a
	// second one.
	r.Get("/chat/{id}/resume", h.resume)
	r.Post("/chat/{id}/tools/{messageID}/{decision}", h.resolveTool)

	r.Post("/chat/{id}/delete", h.deleteConversation)
}

// index lists conversations, opening the most recent one.
func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	list, err := h.svc.Conversations().List(r.Context(), user.ID, 50)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	if len(list) > 0 {
		http.Redirect(w, r, "/app/chat/"+list[0].ID.String(), http.StatusSeeOther)
		return
	}

	stats, err := buildCoachStats(r.Context(), h.svc.Conversations(), nil, time.Now())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	render(w, r, http.StatusOK, chatpages.Empty(user, nil, stats))
}

func (h *Handler) startConversation(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	var (
		conversation conversations.Conversation
		err          error
	)
	if strings.TrimSpace(r.PostFormValue("kind")) == conversations.KindReflection {
		conversation, err = h.svc.StartReflection(r.Context(), user.ID)
	} else {
		conversation, err = h.svc.StartConversation(r.Context(), user.ID)
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, conversationURL(conversation.ID, r.PostFormValue("draft")), http.StatusSeeOther)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	conversation, messages, list, err := h.load(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	stats, err := buildCoachStats(r.Context(), h.svc.Conversations(), list, time.Now())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	// Rendered from the stored turn rather than pushed down the stream: the
	// stream that suspended has already closed, and its sse:done handler
	// re-fetches this page. So the card only has to exist here to appear at the
	// right moment.
	pending, err := h.pendingTools(r, conversation.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	// A trailing tool-result turn means the model was handed an answer and has
	// not replied yet, which is exactly the state an approval leaves behind.
	// Derived rather than carried in the URL, so a refresh does the right thing
	// and a stale ?resume=1 cannot open a stream that is not owed.
	resuming := len(pending) == 0 && len(messages) > 0 && len(messages[len(messages)-1].ToolResults) > 0

	render(w, r, http.StatusOK, chatpages.Page(user, conversation, messages, list, stats, pending, resuming, clipDraft(r.URL.Query().Get("draft"))))
}

// pendingTools renders the waiting call, if there is one, as something a person
// can read before allowing it.
func (h *Handler) pendingTools(r *http.Request, conversationID uuid.UUID) ([]chatpages.PendingTool, error) {
	user := auth.MustUser(r.Context())

	waiting, ok, err := h.svc.PendingApproval(r.Context(), user, conversationID)
	if err != nil || !ok {
		return nil, err
	}

	out := make([]chatpages.PendingTool, 0, len(waiting.Calls))
	for _, call := range waiting.Calls {
		out = append(out, chatpages.PendingTool{
			MessageID: waiting.MessageID,
			Name:      call.Name,
			Summary:   describeCall(call),
		})
	}
	return out, nil
}

// resolveTool records the person's answer and sends them back to the page,
// where the resumed reply streams in.
func (h *Handler) resolveTool(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	messageID, err := uuid.Parse(chi.URLParam(r, "messageID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	decision := chi.URLParam(r, "decision")
	if decision != "approve" && decision != "decline" {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	// The tools run here, inside the POST, before anything redirects. Doing it
	// on the resumed stream instead would mean a refresh could run a write a
	// second time.
	if err = h.svc.ResolvePending(r.Context(), user, conversationID, messageID, decision == "approve"); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/chat/"+conversationID.String(), http.StatusSeeOther)
}

// resume streams the rest of a reply whose turn stopped for approval.
func (h *Handler) resume(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	log := middleware.FromContext(r.Context())

	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	_ = rc.Flush()

	stream, err := h.svc.Resume(r.Context(), user, conversationID)
	if err != nil {
		writeEvent(w, rc, "error", chatpages.StreamErrorHTML(friendly(err)))
		writeEvent(w, rc, "done", "")
		return
	}

	for chunk := range stream {
		if chunk.Err != nil {
			log.Error("resumed coach stream failed", slog.Any("error", chunk.Err))
			writeEvent(w, rc, "error", chatpages.StreamErrorHTML(friendly(chunk.Err)))
			break
		}
		if chunk.Text == "" {
			continue
		}
		writeEvent(w, rc, "token", chatpages.TokenHTML(chunk.Text))
	}

	writeEvent(w, rc, "done", "")
}

// sendMessage stores the message and returns the two bubbles that HTMX appends:
// the user's turn, and an empty assistant turn that opens the SSE stream.
//
// The reply is not generated here. A POST that blocked for thirty seconds while
// a model wrote would be a request that times out, so the work happens on the
// stream the returned fragment connects to.
func (h *Handler) sendMessage(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	if err = r.ParseMultipartForm(maxChatUpload); err != nil {
		if err = r.ParseForm(); err != nil {
			h.fail(w, r, apperr.ErrValidation)
			return
		}
	}

	text := strings.TrimSpace(r.PostFormValue("message"))
	attachment, attErr := h.readAttachment(r, user.ID)
	if attErr != nil {
		render(w, r, http.StatusUnprocessableEntity, chatpages.ComposerError(conversationID, text, attErr.Error()))
		return
	}

	if err = conversations.ValidateTurn(text, attachment != nil); err != nil {
		// The composer keeps what was typed; nothing was stored.
		render(w, r, http.StatusUnprocessableEntity, chatpages.ComposerError(conversationID, text, "Type something first."))
		return
	}

	conversation, err := h.svc.Conversations().Get(r.Context(), conversationID, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if conversation.Ended() {
		h.fail(w, r, apperr.Wrap(apperr.ErrConflict, "this reflection has ended"))
		return
	}

	mediaID := uuid.Nil
	if attachment != nil {
		mediaID = attachment.MediaID
	}
	render(w, r, http.StatusOK, chatpages.PendingExchange(conversation.ID, text, mediaID))
}

func (h *Handler) readAttachment(r *http.Request, userID uuid.UUID) (*conversations.Attachment, error) {
	if h.images == nil || r.MultipartForm == nil {
		return nil, nil
	}

	file, header, err := r.FormFile("attachment")
	if err != nil {
		// No file chosen is the common case, not an error.
		if err == http.ErrMissingFile {
			return nil, nil
		}
		return nil, fmt.Errorf("could not read that file")
	}
	defer func() { _ = file.Close() }()

	stored, err := h.images.StoreChatImage(r.Context(), userID, header.Filename, header.Size, file)
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			return nil, fmt.Errorf("%s", fieldErrs.Messages()["attachment"])
		}
		return nil, fmt.Errorf("could not store that photo")
	}

	return &conversations.Attachment{
		MediaID:  stored.ID,
		Kind:     stored.Kind,
		MIMEType: stored.MIMEType,
		Name:     stored.OriginalName,
	}, nil
}

// stream generates the reply and pushes it as Server-Sent Events.
//
// The message the stream answers is the last user turn, which sendMessage has
// already stored. Passing the text through the query string instead would put
// user content in a URL, where it lands in access logs.
func (h *Handler) stream(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	log := middleware.FromContext(r.Context())

	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	conversation, err := h.svc.Conversations().Get(r.Context(), conversationID, user.ID)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	begin := r.URL.Query().Get("begin") == "1"
	pending := strings.TrimSpace(r.URL.Query().Get("m"))
	mediaID, _ := uuid.Parse(r.URL.Query().Get("media"))
	if !begin && pending == "" && mediaID == uuid.Nil {
		http.Error(w, "nothing to answer", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Without this, nginx and most reverse proxies buffer the whole response
	// and the user sees nothing until the coach has finished writing.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// ResponseController rather than a ResponseWriter type assertion: the
	// logging middleware wraps the writer, and Flusher would be lost.
	rc := http.NewResponseController(w)
	_ = rc.Flush()

	// The budget is spent here rather than on the composer POST, because this is
	// the request that reaches the model. Guarding the POST instead would leave
	// this route reachable on its own — it generates from stored history, so it
	// is real spend — and guarding both would charge two for every turn.
	//
	// The refusal goes out as an SSE frame rather than a 429, because the
	// response has already been committed as an event stream by this point. It
	// takes the same path a provider failure takes, which the page already knows
	// how to display.
	decision, err := h.quotas.Consume(r.Context(), user.ID, quota.CoachMessage)
	if err == nil && !decision.Allowed {
		log.Warn("coach message refused by quota",
			slog.String("user_id", user.ID.String()),
			slog.Duration("retry_after", decision.RetryAfter))
		writeEvent(w, rc, "error", chatpages.StreamErrorHTML(quotaMessage(decision)))
		writeEvent(w, rc, "done", "")
		return
	}

	// Both branches reach a model, so the budget is spent before either one is
	// chosen: a reflection is generated from stored history and costs the same
	// as an answer to a typed message.
	var stream <-chan ai.StreamChunk
	if begin {
		stream, err = h.svc.BeginReflection(r.Context(), user, conversation.ID)
	} else {
		in := Incoming{Text: pending}
		if mediaID != uuid.Nil {
			in.Attachments = []conversations.Attachment{{
				MediaID: mediaID,
				Kind:    "image",
			}}
			if h.images != nil {
				if rec, recErr := h.images.LoadChatImage(r.Context(), mediaID, user.ID); recErr == nil {
					in.Attachments[0] = conversations.Attachment{
						MediaID:  rec.ID,
						Kind:     rec.Kind,
						MIMEType: rec.MIMEType,
						Name:     rec.OriginalName,
					}
				}
			}
		}
		stream, err = h.svc.SendIncoming(r.Context(), user, conversation.ID, in)
	}
	if err != nil {
		writeEvent(w, rc, "error", chatpages.StreamErrorHTML(friendly(err)))
		writeEvent(w, rc, "done", "")
		return
	}

	for chunk := range stream {
		if chunk.Err != nil {
			log.Error("coach stream failed", slog.Any("error", chunk.Err))
			writeEvent(w, rc, "error", chatpages.StreamErrorHTML(friendly(chunk.Err)))
			break
		}
		if chunk.Text == "" {
			continue
		}
		writeEvent(w, rc, "token", chatpages.TokenHTML(chunk.Text))
	}

	// Tells the client to swap the streamed bubble for the stored message and
	// close the connection. Without it the browser reconnects forever.
	writeEvent(w, rc, "done", "")
}

func (h *Handler) deleteConversation(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	if err := h.svc.Conversations().Delete(r.Context(), conversationID, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/chat", http.StatusSeeOther)
}

// load fetches everything the chat page renders.
func (h *Handler) load(r *http.Request) (conversations.Conversation, []conversations.Message, []conversations.Conversation, error) {
	user := auth.MustUser(r.Context())

	conversationID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return conversations.Conversation{}, nil, nil, apperr.ErrNotFound
	}

	conversation, err := h.svc.Conversations().Get(r.Context(), conversationID, user.ID)
	if err != nil {
		return conversations.Conversation{}, nil, nil, err
	}

	messages, err := h.svc.Conversations().History(r.Context(), conversation.ID)
	if err != nil {
		return conversations.Conversation{}, nil, nil, err
	}

	list, err := h.svc.Conversations().List(r.Context(), user.ID, 50)
	if err != nil {
		return conversations.Conversation{}, nil, nil, err
	}

	return conversation, messages, list, nil
}

// writeEvent emits one SSE frame.
//
// HTML fragments are sent rather than JSON, so the browser needs no rendering
// logic — HTMX drops each frame straight into the page. Newlines are escaped
// because a bare newline inside data: would terminate the frame early and
// truncate the coach mid-sentence.
func writeEvent(w http.ResponseWriter, rc *http.ResponseController, event, data string) {
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, strings.ReplaceAll(data, "\n", "\\n"))
	_ = rc.Flush()
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	log := middleware.FromContext(r.Context())

	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	case apperr.Is(err, apperr.ErrConflict):
		http.Error(w, "This reflection has ended.", http.StatusConflict)
	default:
		log.Error("chat request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}

// friendly turns an internal error into something worth showing a user.
func friendly(err error) string {
	switch {
	case apperr.Is(err, apperr.ErrUnavailable):
		return "The coach is busy right now. Try again in a moment."
	case apperr.Is(err, apperr.ErrForbidden):
		return "Khepri cannot reach its AI provider. Check the server configuration."
	case apperr.Is(err, apperr.ErrConflict):
		return "This reflection has ended."
	default:
		return "Something went wrong while writing that reply."
	}
}

// quotaMessage says a refusal out loud, in the same voice friendly uses.
//
// It names a wait rather than a limit, because the number a person needs is
// when they can carry on — not how the bound was configured.
func quotaMessage(decision quota.Decision) string {
	switch {
	case decision.RetryAfter < time.Minute:
		return "You have reached your coach message limit. Try again in less than a minute."
	case decision.RetryAfter < time.Hour:
		return fmt.Sprintf("You have reached your coach message limit. Try again in %d minutes.", int(decision.RetryAfter.Minutes()))
	default:
		return "You have reached your coach message limit. Try again in about an hour."
	}
}
