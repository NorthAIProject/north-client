package crews

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/moderation"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/htmx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	crewpages "github.com/NorthAIProject/north-client/web/crews"
)

// chatRoutes are the crew space: its pages, the fragments the live stream
// asks for, and what members post.
func (h *Handler) chatRoutes(r chi.Router) {
	r.Get("/crews/{crewID}/c/{channelID}", h.channelPage)
	r.Get("/crews/{crewID}/challenge", h.challengePage)
	r.Get("/crews/{crewID}/channels", h.channelsFragment)
	r.Get("/crews/{crewID}/c/{channelID}/messages", h.messagesFragment)
	r.Get("/crews/{crewID}/c/{channelID}/pins", h.pinsFragment)
	r.Get("/crews/{crewID}/m/{messageID}", h.messageFragment)
	r.Get("/crews/{crewID}/m/{messageID}/edit", h.editFragment)

	r.Post("/crews/{crewID}/c/{channelID}/messages", h.send)
	r.Post("/crews/{crewID}/c/{channelID}/read", h.markRead)
	r.Post("/crews/{crewID}/m/{messageID}/edit", h.edit)
	r.Post("/crews/{crewID}/m/{messageID}/delete", h.deleteMessage)
	r.Post("/crews/{crewID}/m/{messageID}/react", h.react)
	r.Post("/crews/{crewID}/m/{messageID}/pin", h.pin)
	r.Post("/crews/{crewID}/report", h.report)
	r.Post("/crews/{crewID}/channels", h.createChannel)
	r.Post("/crews/{crewID}/c/{channelID}/edit", h.updateChannel)
	r.Post("/crews/{crewID}/c/{channelID}/delete", h.deleteChannel)
	r.Post("/crews/{crewID}/notifications", h.setNotify)
}

// space is /app/crews/{id}: the default channel's chat.
func (h *Handler) space(w http.ResponseWriter, r *http.Request) {
	crewID, ok := h.pathUUID(w, r, "crewID")
	if !ok {
		return
	}
	h.renderSpace(w, r, crewID, uuid.Nil, crewpages.TabChat)
}

func (h *Handler) channelPage(w http.ResponseWriter, r *http.Request) {
	crewID, ok := h.pathUUID(w, r, "crewID")
	if !ok {
		return
	}
	channelID, ok := h.pathUUID(w, r, "channelID")
	if !ok {
		return
	}
	h.renderSpace(w, r, crewID, channelID, crewpages.TabChat)
}

func (h *Handler) challengePage(w http.ResponseWriter, r *http.Request) {
	crewID, ok := h.pathUUID(w, r, "crewID")
	if !ok {
		return
	}
	h.renderSpace(w, r, crewID, uuid.Nil, crewpages.TabChallenge)
}

// renderSpace draws the space with channelID open, or the default channel
// when it is nil.
func (h *Handler) renderSpace(w http.ResponseWriter, r *http.Request, crewID, channelID uuid.UUID, tab string) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	sp, err := h.svc.Space(ctx, crewID, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	open, found := openChannel(sp, channelID)
	if !found {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	mine, err := h.svc.Mine(ctx, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	unread, err := h.chat.Unread(ctx, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	v := crewpages.SpaceView{
		Space: sp, Mine: mine, Unread: map[uuid.UUID]bool{}, Channel: open, Tab: tab,
		JoinURL: JoinURL(h.siteURL, sp.Code), Me: user.ID, Loc: user.Location(),
	}
	for _, u := range unread {
		v.Unread[u.CrewID] = true
	}
	if tab == crewpages.TabChallenge {
		if v.Board, err = h.svc.Board(ctx, crewID, user.ID); err != nil {
			h.fail(w, r, err)
			return
		}
	} else if v.Page, err = h.chat.List(ctx, crewID, open.ID, user.ID, 0, 0, 0); err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, crewpages.SpacePage(user, v))
}

// openChannel is channelID among the space's channels, or the default one
// when channelID is nil.
func openChannel(sp Space, channelID uuid.UUID) (SpaceChannel, bool) {
	for _, ch := range sp.Channels {
		if ch.ID == channelID || (channelID == uuid.Nil && ch.IsDefault) {
			return ch, true
		}
	}
	return SpaceChannel{}, false
}

func (h *Handler) channelsFragment(w http.ResponseWriter, r *http.Request) {
	crewID, ok := h.pathUUID(w, r, "crewID")
	if !ok {
		return
	}
	user := auth.MustUser(r.Context())
	sp, err := h.svc.Space(r.Context(), crewID, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	open, _ := uuid.Parse(r.URL.Query().Get("channel"))
	h.render(w, r, http.StatusOK, crewpages.ChannelList(crewpages.ChannelListView{
		CrewID: crewID, Channels: sp.Channels, Open: open, CanManage: sp.MyRole.CanManage(),
	}))
}

// messagesFragment is what is new after the page's cursor, or with before=
// the page older than the oldest row shown.
func (h *Handler) messagesFragment(w http.ResponseWriter, r *http.Request) {
	v, ok := h.chatView(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	before, err1 := queryInt(q.Get("before"))
	after, err2 := queryInt(q.Get("after"))
	if err1 != nil || err2 != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	page, err := h.chat.List(r.Context(), v.CrewID, v.Channel.ID, v.Me, before, after, 0)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if before > 0 {
		h.render(w, r, http.StatusOK, crewpages.Older(v, page))
		return
	}
	cursor := after
	if n := len(page.Messages); n > 0 {
		cursor = page.Messages[n-1].Seq
	}
	h.render(w, r, http.StatusOK, crewpages.Messages(v, page.Messages, cursor))
}

func (h *Handler) pinsFragment(w http.ResponseWriter, r *http.Request) {
	v, ok := h.chatView(w, r)
	if !ok {
		return
	}
	pins, err := h.chat.Pins(r.Context(), v.CrewID, v.Channel.ID, v.Me)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, crewpages.Pins(v, pins))
}

// messageFragment is one message again, after the live stream said it
// changed. A message the viewer may no longer see answers empty, which takes
// its row off the page.
func (h *Handler) messageFragment(w http.ResponseWriter, r *http.Request) {
	v, m, ok := h.message(w, r)
	if !ok {
		return
	}
	if m == nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	h.render(w, r, http.StatusOK, crewpages.Message(v, *m))
}

func (h *Handler) editFragment(w http.ResponseWriter, r *http.Request) {
	v, m, ok := h.message(w, r)
	if !ok {
		return
	}
	if m == nil || m.Author == nil || m.Author.ID != v.Me {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	h.render(w, r, http.StatusOK, crewpages.MessageEdit(v, *m, ""))
}

func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	v, ok := h.chatView(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	in := SendInput{Body: r.PostFormValue("body")}
	if id, err := uuid.Parse(r.PostFormValue("reply_to")); err == nil {
		in.ReplyToID = &id
	}
	if _, err := h.chat.Send(r.Context(), v.CrewID, v.Channel.ID, v.Me, in); err != nil {
		h.refusePost(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// refusePost answers the composer with why a post failed, in the slot under
// it. Not having accepted the rules carries the marker that opens them.
//
// Every refusal is a 422, including the two that are really "forbidden": the
// layout's htmx config discards 4xx responses other than 422 and 429, so a
// 403 would leave the composer silent. The API keeps the 403s.
func (h *Handler) refusePost(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	var fields apperr.FieldErrors
	switch {
	case errors.Is(err, moderation.ErrTermsRequired):
		h.render(w, r, http.StatusUnprocessableEntity, crewpages.ComposerRefusal(i18n.T(ctx, "crews.err.terms"), true))
	case errors.Is(err, moderation.ErrSuspended):
		h.render(w, r, http.StatusUnprocessableEntity, crewpages.ComposerRefusal(i18n.T(ctx, "crews.err.suspended"), false))
	case apperr.As(err, &fields):
		h.render(w, r, http.StatusUnprocessableEntity, crewpages.ComposerRefusal(firstMessage(fields), false))
	default:
		h.fail(w, r, err)
	}
}

func (h *Handler) markRead(w http.ResponseWriter, r *http.Request) {
	v, ok := h.chatView(w, r)
	if !ok {
		return
	}
	seq, err := strconv.ParseInt(r.PostFormValue("after"), 10, 64)
	if err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	if err = h.chat.MarkRead(r.Context(), v.CrewID, v.Channel.ID, v.Me, seq); err != nil {
		h.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) edit(w http.ResponseWriter, r *http.Request) {
	v, m, ok := h.message(w, r)
	if !ok {
		return
	}
	if m == nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	body := r.PostFormValue("body")
	edited, err := h.chat.Edit(r.Context(), v.CrewID, m.ID, v.Me, body)
	var fields apperr.FieldErrors
	switch {
	case err == nil:
		h.render(w, r, http.StatusOK, crewpages.Message(v, edited))
	case apperr.As(err, &fields):
		m.Body = body
		h.render(w, r, http.StatusUnprocessableEntity, crewpages.MessageEdit(v, *m, firstMessage(fields)))
	case errors.Is(err, moderation.ErrTermsRequired), errors.Is(err, moderation.ErrSuspended):
		m.Body = body
		h.render(w, r, http.StatusUnprocessableEntity, crewpages.MessageEdit(v, *m, i18n.T(r.Context(), "crews.err.suspended")))
	default:
		h.fail(w, r, err)
	}
}

func (h *Handler) deleteMessage(w http.ResponseWriter, r *http.Request) {
	crewID, messageID, ok := h.crewAnd(w, r, "messageID")
	if !ok {
		return
	}
	h.done(w, r, h.chat.Delete(r.Context(), crewID, messageID, auth.MustUser(r.Context()).ID))
}

func (h *Handler) react(w http.ResponseWriter, r *http.Request) {
	crewID, messageID, ok := h.crewAnd(w, r, "messageID")
	if !ok {
		return
	}
	on := r.PostFormValue("on") != "false"
	h.done(w, r, h.chat.React(r.Context(), crewID, messageID, auth.MustUser(r.Context()).ID, r.PostFormValue("emoji"), on))
}

func (h *Handler) pin(w http.ResponseWriter, r *http.Request) {
	crewID, messageID, ok := h.crewAnd(w, r, "messageID")
	if !ok {
		return
	}
	on := r.PostFormValue("on") != "false"
	h.done(w, r, h.chat.Pin(r.Context(), crewID, messageID, auth.MustUser(r.Context()).ID, on))
}

// report takes the message from the form: one dialog serves every row.
func (h *Handler) report(w http.ResponseWriter, r *http.Request) {
	crewID, ok := h.pathUUID(w, r, "crewID")
	if !ok {
		return
	}
	messageID, err := uuid.Parse(r.PostFormValue("message"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	err = h.chat.Report(r.Context(), crewID, messageID, auth.MustUser(r.Context()).ID, r.PostFormValue("reason"), r.PostFormValue("note"))
	h.done(w, r, err)
}

func (h *Handler) createChannel(w http.ResponseWriter, r *http.Request) {
	crewID, ok := h.pathUUID(w, r, "crewID")
	if !ok {
		return
	}
	ch, err := h.svc.CreateChannel(r.Context(), crewID, auth.MustUser(r.Context()).ID, r.PostFormValue("name"), r.PostFormValue("topic"))
	if err != nil {
		h.done(w, r, err)
		return
	}
	goTo(w, r, "/app/crews/"+crewID.String()+"/c/"+ch.ID.String())
}

func (h *Handler) updateChannel(w http.ResponseWriter, r *http.Request) {
	crewID, channelID, ok := h.crewAnd(w, r, "channelID")
	if !ok {
		return
	}
	if _, err := h.svc.UpdateChannel(r.Context(), crewID, channelID, auth.MustUser(r.Context()).ID, r.PostFormValue("name"), r.PostFormValue("topic")); err != nil {
		h.done(w, r, err)
		return
	}
	goTo(w, r, "/app/crews/"+crewID.String()+"/c/"+channelID.String())
}

func (h *Handler) deleteChannel(w http.ResponseWriter, r *http.Request) {
	crewID, channelID, ok := h.crewAnd(w, r, "channelID")
	if !ok {
		return
	}
	if err := h.svc.DeleteChannel(r.Context(), crewID, channelID, auth.MustUser(r.Context()).ID); err != nil {
		h.done(w, r, err)
		return
	}
	goTo(w, r, "/app/crews/"+crewID.String())
}

// setNotify saves which messages push the member, and mutes them for a while
// when they asked to.
func (h *Handler) setNotify(w http.ResponseWriter, r *http.Request) {
	crewID, ok := h.pathUUID(w, r, "crewID")
	if !ok {
		return
	}
	var until *time.Time
	if d, muted := muteFor[r.PostFormValue("mute")]; muted {
		t := time.Now().Add(d)
		until = &t
	}
	if err := h.svc.SetNotify(r.Context(), crewID, auth.MustUser(r.Context()).ID, r.PostFormValue("notify"), until); err != nil {
		h.fail(w, r, err)
		return
	}
	next := r.PostFormValue("next")
	if !auth.SafeRedirect(next) {
		next = "/app/crews/" + crewID.String()
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

var muteFor = map[string]time.Duration{"hour": time.Hour, "day": 24 * time.Hour, "week": 7 * 24 * time.Hour}

// chatView is the crew and channel in the path, checked for membership, as
// the transcript fragments see them.
func (h *Handler) chatView(w http.ResponseWriter, r *http.Request) (crewpages.ChatView, bool) {
	crewID, channelID, ok := h.crewAnd(w, r, "channelID")
	if !ok {
		return crewpages.ChatView{}, false
	}
	user := auth.MustUser(r.Context())
	_, role, err := h.chat.member(r.Context(), crewID, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return crewpages.ChatView{}, false
	}
	return crewpages.ChatView{CrewID: crewID, Channel: SpaceChannel{Channel: Channel{ID: channelID}}, Me: user.ID, MyRole: role, Loc: user.Location()}, true
}

// message is the message in the path as the viewer sees it; nil when they
// may not see it.
func (h *Handler) message(w http.ResponseWriter, r *http.Request) (crewpages.ChatView, *Message, bool) {
	crewID, messageID, ok := h.crewAnd(w, r, "messageID")
	if !ok {
		return crewpages.ChatView{}, nil, false
	}
	user := auth.MustUser(r.Context())
	_, role, err := h.chat.member(r.Context(), crewID, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return crewpages.ChatView{}, nil, false
	}
	v := crewpages.ChatView{CrewID: crewID, Me: user.ID, MyRole: role, Loc: user.Location()}
	m, err := h.chat.Get(r.Context(), crewID, messageID, user.ID)
	if apperr.Is(err, apperr.ErrNotFound) {
		return v, nil, true
	}
	if err != nil {
		h.fail(w, r, err)
		return crewpages.ChatView{}, nil, false
	}
	v.Channel = SpaceChannel{Channel: Channel{ID: m.ChannelID}}
	return v, &m, true
}

// done answers a write the page does not swap: 204, or the refusal's words
// for the form's error slot.
func (h *Handler) done(w http.ResponseWriter, r *http.Request, err error) {
	var fields apperr.FieldErrors
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case apperr.As(err, &fields):
		h.render(w, r, http.StatusUnprocessableEntity, crewpages.FormRefusal(firstMessage(fields)))
	default:
		h.fail(w, r, err)
	}
}

func (h *Handler) pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) crewAnd(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, uuid.UUID, bool) {
	crewID, ok := h.pathUUID(w, r, "crewID")
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	id, ok := h.pathUUID(w, r, name)
	return crewID, id, ok
}

// goTo sends the browser to a page: HX-Redirect for an htmx form in a
// dialog, See Other otherwise.
func goTo(w http.ResponseWriter, r *http.Request, to string) {
	if htmx.IsRequest(r) {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func firstMessage(fields apperr.FieldErrors) string {
	for _, msg := range fields.Messages() {
		return msg
	}
	return ""
}
