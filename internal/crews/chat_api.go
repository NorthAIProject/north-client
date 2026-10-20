package crews

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/moderation"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// chatRoutes are a crew's chat for native clients: the space, its channels,
// their messages, and what members do to messages.
func (a *API) chatRoutes(r chi.Router) {
	r.Get("/crews/unread", a.unread)
	r.Get("/crews/{crewID}/space", a.space)
	r.Put("/crews/{crewID}/notifications", a.setNotify)
	r.Post("/crews/{crewID}/channels", a.createChannel)
	r.Patch("/crews/{crewID}/channels/{channelID}", a.updateChannel)
	r.Delete("/crews/{crewID}/channels/{channelID}", a.deleteChannel)
	r.Get("/crews/{crewID}/channels/{channelID}/messages", a.messages)
	r.Post("/crews/{crewID}/channels/{channelID}/messages", a.send)
	r.Get("/crews/{crewID}/channels/{channelID}/pins", a.pins)
	r.Post("/crews/{crewID}/channels/{channelID}/read", a.markRead)
	r.Get("/crews/{crewID}/messages/{messageID}", a.message)
	r.Patch("/crews/{crewID}/messages/{messageID}", a.edit)
	r.Delete("/crews/{crewID}/messages/{messageID}", a.deleteMessage)
	r.Put("/crews/{crewID}/messages/{messageID}/reactions/{emoji}", a.react(true))
	r.Delete("/crews/{crewID}/messages/{messageID}/reactions/{emoji}", a.react(false))
	r.Put("/crews/{crewID}/messages/{messageID}/pin", a.pin(true))
	r.Delete("/crews/{crewID}/messages/{messageID}/pin", a.pin(false))
	r.Post("/crews/{crewID}/messages/{messageID}/report", a.report)
}

// CrewPerson is someone in a crew. Role is owner, admin or member on the
// member list and absent on a message author; treat unknown roles as member.
type CrewPerson struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name,omitempty"`
	Handle string    `json:"handle,omitempty"`
	Role   string    `json:"role,omitempty"`
}

type CrewChannelView struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name,omitempty"`
	Topic string    `json:"topic,omitempty"`
	// Kind is text today; board and wins come later. Show unknown kinds as
	// text or hide them.
	Kind     string `json:"kind,omitempty"`
	Default  bool   `json:"default,omitempty"`
	Unread   int    `json:"unread,omitempty"`
	Mentions int    `json:"mentions,omitempty"`
}

type CrewSpaceView struct {
	Crew   CrewView `json:"crew"`
	MyRole string   `json:"myRole,omitempty"`
	// Notify is all, mentions or none.
	Notify     string            `json:"notify,omitempty"`
	MutedUntil *time.Time        `json:"mutedUntil,omitempty"`
	Channels   []CrewChannelView `json:"channels"`
	Members    []CrewPerson      `json:"members"`
}

type CrewQuoteView struct {
	ID          uuid.UUID `json:"id"`
	AuthorName  string    `json:"authorName,omitempty"`
	Excerpt     string    `json:"excerpt,omitempty"`
	Unavailable bool      `json:"unavailable,omitempty"`
}

type CrewReactionView struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Mine  bool   `json:"mine,omitempty"`
}

type CrewMessageView struct {
	ID        uuid.UUID `json:"id"`
	Seq       int64     `json:"seq,omitempty"`
	ChannelID uuid.UUID `json:"channelId,omitzero"`
	// Kind is text or system; render unknown kinds as "Update Khepri to see
	// this". A system message has no author and its body is already in the
	// reader's language.
	Kind       string             `json:"kind,omitempty"`
	Body       string             `json:"body"`
	Author     *CrewPerson        `json:"author"`
	ReplyTo    *CrewQuoteView     `json:"replyTo"`
	MentionsMe bool               `json:"mentionsMe,omitempty"`
	Reactions  []CrewReactionView `json:"reactions,omitempty"`
	Pinned     bool               `json:"pinned,omitempty"`
	Edited     bool               `json:"edited,omitempty"`
	Deleted    bool               `json:"deleted,omitempty"`
	CreatedAt  time.Time          `json:"createdAt,omitzero"`
}

type CrewMessagePage struct {
	Messages []CrewMessageView `json:"messages"`
	HasMore  bool              `json:"hasMore,omitempty"`
}

type CrewMessageList struct {
	Messages []CrewMessageView `json:"messages"`
}

type CrewUnreadView struct {
	CrewID   uuid.UUID `json:"crewId"`
	Unread   int       `json:"unread,omitempty"`
	Mentions int       `json:"mentions,omitempty"`
}

type CrewUnreadList struct {
	Crews []CrewUnreadView `json:"crews"`
	Total int              `json:"total"`
}

type ChannelRequest struct {
	Name  string `json:"name"`
	Topic string `json:"topic"`
}

type SendMessageRequest struct {
	Body      string     `json:"body"`
	ReplyToID *uuid.UUID `json:"replyToId"`
	ClientID  *uuid.UUID `json:"clientId"`
}

type EditMessageRequest struct {
	Body string `json:"body"`
}

type ReadRequest struct {
	Seq int64 `json:"seq"`
}

type ReportRequest struct {
	Reason string `json:"reason"`
	Note   string `json:"note"`
}

type NotifyRequest struct {
	Notify     string     `json:"notify"`
	MutedUntil *time.Time `json:"mutedUntil"`
}

// messageBodyLimit fits a 4000-character message in any script, with room
// for the JSON around it.
const messageBodyLimit = 16 << 10

func (a *API) space(w http.ResponseWriter, r *http.Request) {
	crewID, ok := pathID(w, r, "crewID")
	if !ok {
		return
	}
	sp, err := a.svc.Space(r.Context(), crewID, auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "The crew could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.projectSpace(sp))
}

func (a *API) setNotify(w http.ResponseWriter, r *http.Request) {
	crewID, ok := pathID(w, r, "crewID")
	if !ok {
		return
	}
	var req NotifyRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.svc.SetNotify(r.Context(), crewID, auth.MustUser(r.Context()).ID, req.Notify, req.MutedUntil); err != nil {
		httpx.Error(w, err, "The notification setting could not be saved.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) createChannel(w http.ResponseWriter, r *http.Request) {
	crewID, ok := pathID(w, r, "crewID")
	if !ok {
		return
	}
	var req ChannelRequest
	if !readJSON(w, r, &req) {
		return
	}
	ch, err := a.svc.CreateChannel(r.Context(), crewID, auth.MustUser(r.Context()).ID, req.Name, req.Topic)
	if err != nil {
		httpx.Error(w, err, "The channel could not be made.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, projectChannel(SpaceChannel{Channel: ch}))
}

func (a *API) updateChannel(w http.ResponseWriter, r *http.Request) {
	crewID, channelID, ok := twoIDs(w, r, "channelID")
	if !ok {
		return
	}
	var req ChannelRequest
	if !readJSON(w, r, &req) {
		return
	}
	ch, err := a.svc.UpdateChannel(r.Context(), crewID, channelID, auth.MustUser(r.Context()).ID, req.Name, req.Topic)
	if err != nil {
		httpx.Error(w, err, "The channel could not be changed.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectChannel(SpaceChannel{Channel: ch}))
}

func (a *API) deleteChannel(w http.ResponseWriter, r *http.Request) {
	crewID, channelID, ok := twoIDs(w, r, "channelID")
	if !ok {
		return
	}
	if err := a.svc.DeleteChannel(r.Context(), crewID, channelID, auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "The channel could not be deleted.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// messages pages a channel: before= for older history, after= for what is
// new since the last message the client has; neither for the latest page.
func (a *API) messages(w http.ResponseWriter, r *http.Request) {
	crewID, channelID, ok := twoIDs(w, r, "channelID")
	if !ok {
		return
	}
	q := r.URL.Query()
	before, err1 := queryInt(q.Get("before"))
	after, err2 := queryInt(q.Get("after"))
	limit, err3 := queryInt(q.Get("limit"))
	if err := errors.Join(err1, err2, err3); err != nil {
		httpx.Error(w, apperr.FieldErrors{}.Add("cursor", "before, after and limit are whole numbers"), "The page could not be read.")
		return
	}
	page, err := a.chat.List(r.Context(), crewID, channelID, auth.MustUser(r.Context()).ID, before, after, int(limit))
	if err != nil {
		httpx.Error(w, err, "The messages could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectPage(r.Context(), page))
}

func (a *API) send(w http.ResponseWriter, r *http.Request) {
	crewID, channelID, ok := twoIDs(w, r, "channelID")
	if !ok {
		return
	}
	var req SendMessageRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: messageBodyLimit}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	m, err := a.chat.Send(r.Context(), crewID, channelID, auth.MustUser(r.Context()).ID, SendInput{Body: req.Body, ReplyToID: req.ReplyToID, ClientID: req.ClientID})
	if err != nil {
		postError(w, err, "The message could not be sent.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, projectMessage(r.Context(), m))
}

func (a *API) pins(w http.ResponseWriter, r *http.Request) {
	crewID, channelID, ok := twoIDs(w, r, "channelID")
	if !ok {
		return
	}
	pins, err := a.chat.Pins(r.Context(), crewID, channelID, auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "The pins could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CrewMessageList{Messages: projectMessages(r.Context(), pins)})
}

func (a *API) markRead(w http.ResponseWriter, r *http.Request) {
	crewID, channelID, ok := twoIDs(w, r, "channelID")
	if !ok {
		return
	}
	var req ReadRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.chat.MarkRead(r.Context(), crewID, channelID, auth.MustUser(r.Context()).ID, req.Seq); err != nil {
		httpx.Error(w, err, "That could not be marked read.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) message(w http.ResponseWriter, r *http.Request) {
	crewID, messageID, ok := twoIDs(w, r, "messageID")
	if !ok {
		return
	}
	m, err := a.chat.Get(r.Context(), crewID, messageID, auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "The message could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectMessage(r.Context(), m))
}

func (a *API) edit(w http.ResponseWriter, r *http.Request) {
	crewID, messageID, ok := twoIDs(w, r, "messageID")
	if !ok {
		return
	}
	var req EditMessageRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: messageBodyLimit}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	m, err := a.chat.Edit(r.Context(), crewID, messageID, auth.MustUser(r.Context()).ID, req.Body)
	if err != nil {
		postError(w, err, "The message could not be changed.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectMessage(r.Context(), m))
}

func (a *API) deleteMessage(w http.ResponseWriter, r *http.Request) {
	crewID, messageID, ok := twoIDs(w, r, "messageID")
	if !ok {
		return
	}
	if err := a.chat.Delete(r.Context(), crewID, messageID, auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "The message could not be deleted.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) react(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		crewID, messageID, ok := twoIDs(w, r, "messageID")
		if !ok {
			return
		}
		if err := a.chat.React(r.Context(), crewID, messageID, auth.MustUser(r.Context()).ID, chi.URLParam(r, "emoji"), on); err != nil {
			httpx.Error(w, err, "The reaction could not be saved.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) pin(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		crewID, messageID, ok := twoIDs(w, r, "messageID")
		if !ok {
			return
		}
		if err := a.chat.Pin(r.Context(), crewID, messageID, auth.MustUser(r.Context()).ID, on); err != nil {
			httpx.Error(w, err, "The pin could not be changed.")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *API) report(w http.ResponseWriter, r *http.Request) {
	crewID, messageID, ok := twoIDs(w, r, "messageID")
	if !ok {
		return
	}
	var req ReportRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.chat.Report(r.Context(), crewID, messageID, auth.MustUser(r.Context()).ID, req.Reason, req.Note); err != nil {
		httpx.Error(w, err, "The report could not be sent.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// unread is, per crew, what the caller has not read, for the badge on the
// way in.
func (a *API) unread(w http.ResponseWriter, r *http.Request) {
	list, err := a.chat.Unread(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "Unread counts could not be loaded.")
		return
	}
	out := CrewUnreadList{Crews: []CrewUnreadView{}}
	at := map[uuid.UUID]int{}
	for _, u := range list {
		i, seen := at[u.CrewID]
		if !seen {
			i = len(out.Crews)
			at[u.CrewID] = i
			out.Crews = append(out.Crews, CrewUnreadView{CrewID: u.CrewID})
		}
		out.Crews[i].Unread += u.Count
		out.Crews[i].Mentions += u.Mentions
		out.Total += u.Count
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// unreadByCrew sums the caller's unread channels per crew. Without a chat
// (an API built for tests of the board alone) there is nothing unread.
func (a *API) unreadByCrew(r *http.Request) (map[uuid.UUID]CrewUnreadView, error) {
	out := map[uuid.UUID]CrewUnreadView{}
	if a.chat == nil {
		return out, nil
	}
	list, err := a.chat.Unread(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		return nil, err
	}
	for _, u := range list {
		v := out[u.CrewID]
		v.Unread += u.Count
		v.Mentions += u.Mentions
		out[u.CrewID] = v
	}
	return out, nil
}

// postError answers a refused post. Not having accepted the terms and being
// suspended are both 403, so the code tells the client which screen to show.
func postError(w http.ResponseWriter, err error, message string) {
	switch {
	case errors.Is(err, moderation.ErrTermsRequired):
		httpx.WriteJSON(w, http.StatusForbidden, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: "Accept the community terms to post.", Code: "terms_required"}})
	case errors.Is(err, moderation.ErrSuspended):
		httpx.WriteJSON(w, http.StatusForbidden, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: "You can't post right now.", Code: "suspended"}})
	default:
		httpx.Error(w, err, message)
	}
}

func (a *API) projectSpace(sp Space) CrewSpaceView {
	out := CrewSpaceView{
		Crew: a.projectCrew(sp.Crew), MyRole: string(sp.MyRole), Notify: sp.Notify, MutedUntil: sp.MutedUntil,
		Channels: make([]CrewChannelView, 0, len(sp.Channels)), Members: make([]CrewPerson, 0, len(sp.Members)),
	}
	for _, ch := range sp.Channels {
		out.Channels = append(out.Channels, projectChannel(ch))
	}
	for _, m := range sp.Members {
		out.Members = append(out.Members, CrewPerson{ID: m.ID, Name: m.Name, Handle: m.Handle, Role: string(m.Role)})
	}
	return out
}

func projectChannel(ch SpaceChannel) CrewChannelView {
	return CrewChannelView{ID: ch.ID, Name: ch.Name, Topic: ch.Topic, Kind: ch.Kind, Default: ch.IsDefault, Unread: ch.Unread, Mentions: ch.Mentions}
}

func projectPage(ctx context.Context, p Page) CrewMessagePage {
	return CrewMessagePage{Messages: projectMessages(ctx, p.Messages), HasMore: p.HasMore}
}

func projectMessages(ctx context.Context, ms []Message) []CrewMessageView {
	out := make([]CrewMessageView, 0, len(ms))
	for _, m := range ms {
		out = append(out, projectMessage(ctx, m))
	}
	return out
}

func projectMessage(ctx context.Context, m Message) CrewMessageView {
	out := CrewMessageView{
		ID: m.ID, Seq: m.Seq, ChannelID: m.ChannelID, Kind: m.Kind, Body: m.Body, MentionsMe: m.MentionsMe,
		Pinned: m.Pinned, Edited: m.Edited, Deleted: m.Deleted, CreatedAt: m.CreatedAt,
	}
	if m.Kind == MessageKindSystem {
		out.Body = i18n.T(ctx, m.Body)
	}
	if m.Author != nil {
		out.Author = &CrewPerson{ID: m.Author.ID, Name: m.Author.Name, Handle: m.Author.Handle}
	}
	if m.ReplyTo != nil {
		out.ReplyTo = &CrewQuoteView{ID: m.ReplyTo.ID, AuthorName: m.ReplyTo.AuthorName, Excerpt: m.ReplyTo.Excerpt, Unavailable: m.ReplyTo.Unavailable}
	}
	for _, re := range m.Reactions {
		out.Reactions = append(out.Reactions, CrewReactionView{Emoji: re.Emoji, Count: re.Count, Mine: re.Mine})
	}
	return out
}

// twoIDs is the crew and one more id from the path.
func twoIDs(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, uuid.UUID, bool) {
	crewID, ok := pathID(w, r, "crewID")
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	id, ok := pathID(w, r, name)
	return crewID, id, ok
}

// queryInt reads an optional whole number from the query; empty is zero.
func queryInt(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}
