package coach

import (
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
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	chatpages "github.com/NorthAIProject/north-client/web/chat"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Routes mounts the chat endpoints. Must be mounted behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/chat", h.index)
	r.Post("/chat", h.startConversation)
	r.Get("/chat/{id}", h.show)
	r.Post("/chat/{id}/messages", h.sendMessage)
	r.Get("/chat/{id}/stream", h.stream)
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

	http.Redirect(w, r, "/app/chat/"+conversation.ID.String(), http.StatusSeeOther)
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
	render(w, r, http.StatusOK, chatpages.Page(user, conversation, messages, list, stats))
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

	if err = r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	text := strings.TrimSpace(r.PostFormValue("message"))
	if err = conversations.ValidateMessage(text); err != nil {
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

	render(w, r, http.StatusOK, chatpages.PendingExchange(conversation.ID, text))
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
	if !begin && pending == "" {
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

	var stream <-chan ai.StreamChunk
	if begin {
		stream, err = h.svc.BeginReflection(r.Context(), user, conversation.ID)
	} else {
		stream, err = h.svc.SendMessage(r.Context(), user, conversation.ID, pending)
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
		return "North cannot reach its AI provider. Check the server configuration."
	case apperr.Is(err, apperr.ErrConflict):
		return "This reflection has ended."
	default:
		return "Something went wrong while writing that reply."
	}
}
