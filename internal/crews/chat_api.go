package crews

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/moderation"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// registerChat registers a crew's chat for native clients: the space, its
// channels, their messages, and what members do to messages.
func (a *API) registerChat(g *echo.Group) {
	g.GET("/crews/unread", a.unread)
	g.GET("/crews/:crewID/space", a.space)
	g.PUT("/crews/:crewID/notifications", a.setNotify)
	g.POST("/crews/:crewID/channels", a.createChannel)
	g.PATCH("/crews/:crewID/channels/:channelID", a.updateChannel)
	g.DELETE("/crews/:crewID/channels/:channelID", a.deleteChannel)
	g.GET("/crews/:crewID/channels/:channelID/messages", a.messages)
	g.POST("/crews/:crewID/channels/:channelID/messages", a.send)
	g.GET("/crews/:crewID/channels/:channelID/pins", a.pins)
	g.POST("/crews/:crewID/channels/:channelID/read", a.markRead)
	g.GET("/crews/:crewID/messages/:messageID", a.message)
	g.PATCH("/crews/:crewID/messages/:messageID", a.edit)
	g.DELETE("/crews/:crewID/messages/:messageID", a.deleteMessage)
	g.PUT("/crews/:crewID/messages/:messageID/reactions/:emoji", a.react(true))
	g.DELETE("/crews/:crewID/messages/:messageID/reactions/:emoji", a.react(false))
	g.PUT("/crews/:crewID/messages/:messageID/pin", a.pin(true))
	g.DELETE("/crews/:crewID/messages/:messageID/pin", a.pin(false))
	g.POST("/crews/:crewID/messages/:messageID/report", a.report)
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

func (a *API) space(c *echo.Context) error {
	crewID, err := echox.UUIDParam(c, "crewID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	sp, err := a.svc.Space(ctx, crewID, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "The crew could not be loaded.")
	}
	return c.JSON(http.StatusOK, a.projectSpace(sp))
}

func (a *API) setNotify(c *echo.Context) error {
	crewID, err := echox.UUIDParam(c, "crewID")
	if err != nil {
		return err
	}
	var req NotifyRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.SetNotify(ctx, crewID, auth.MustUser(ctx).ID, req.Notify, req.MutedUntil); err != nil {
		return httpx.Fail(err, "The notification setting could not be saved.")
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) createChannel(c *echo.Context) error {
	crewID, err := echox.UUIDParam(c, "crewID")
	if err != nil {
		return err
	}
	var req ChannelRequest
	if err = readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	ch, err := a.svc.CreateChannel(ctx, crewID, auth.MustUser(ctx).ID, req.Name, req.Topic)
	if err != nil {
		return httpx.Fail(err, "The channel could not be made.")
	}
	return c.JSON(http.StatusCreated, projectChannel(SpaceChannel{Channel: ch}))
}

func (a *API) updateChannel(c *echo.Context) error {
	crewID, channelID, err := twoIDs(c, "channelID")
	if err != nil {
		return err
	}
	var req ChannelRequest
	if err = readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	ch, err := a.svc.UpdateChannel(ctx, crewID, channelID, auth.MustUser(ctx).ID, req.Name, req.Topic)
	if err != nil {
		return httpx.Fail(err, "The channel could not be changed.")
	}
	return c.JSON(http.StatusOK, projectChannel(SpaceChannel{Channel: ch}))
}

func (a *API) deleteChannel(c *echo.Context) error {
	crewID, channelID, err := twoIDs(c, "channelID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.DeleteChannel(ctx, crewID, channelID, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "The channel could not be deleted.")
	}
	return c.NoContent(http.StatusNoContent)
}

// messages pages a channel: before= for older history, after= for what is
// new since the last message the client has; neither for the latest page.
func (a *API) messages(c *echo.Context) error {
	crewID, channelID, err := twoIDs(c, "channelID")
	if err != nil {
		return err
	}
	before, err1 := queryInt(c.QueryParam("before"))
	after, err2 := queryInt(c.QueryParam("after"))
	limit, err3 := queryInt(c.QueryParam("limit"))
	if err = errors.Join(err1, err2, err3); err != nil {
		return httpx.Fail(apperr.FieldErrors{}.Add("cursor", "before, after and limit are whole numbers"), "The page could not be read.")
	}
	ctx := c.Request().Context()
	page, err := a.chat.List(ctx, crewID, channelID, auth.MustUser(ctx).ID, before, after, int(limit))
	if err != nil {
		return httpx.Fail(err, "The messages could not be loaded.")
	}
	return c.JSON(http.StatusOK, projectPage(ctx, page))
}

func (a *API) send(c *echo.Context) error {
	crewID, channelID, err := twoIDs(c, "channelID")
	if err != nil {
		return err
	}
	var req SendMessageRequest
	if err = echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: messageBodyLimit}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	m, err := a.chat.Send(ctx, crewID, channelID, auth.MustUser(ctx).ID, SendInput(req))
	if err != nil {
		return postError(c, err, "The message could not be sent.")
	}
	return c.JSON(http.StatusCreated, projectMessage(ctx, m))
}

func (a *API) pins(c *echo.Context) error {
	crewID, channelID, err := twoIDs(c, "channelID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	pins, err := a.chat.Pins(ctx, crewID, channelID, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "The pins could not be loaded.")
	}
	return c.JSON(http.StatusOK, CrewMessageList{Messages: projectMessages(ctx, pins)})
}

func (a *API) markRead(c *echo.Context) error {
	crewID, channelID, err := twoIDs(c, "channelID")
	if err != nil {
		return err
	}
	var req ReadRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.chat.MarkRead(ctx, crewID, channelID, auth.MustUser(ctx).ID, req.Seq); err != nil {
		return httpx.Fail(err, "That could not be marked read.")
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) message(c *echo.Context) error {
	crewID, messageID, err := twoIDs(c, "messageID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	m, err := a.chat.Get(ctx, crewID, messageID, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "The message could not be loaded.")
	}
	return c.JSON(http.StatusOK, projectMessage(ctx, m))
}

func (a *API) edit(c *echo.Context) error {
	crewID, messageID, err := twoIDs(c, "messageID")
	if err != nil {
		return err
	}
	var req EditMessageRequest
	if err = echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: messageBodyLimit}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	m, err := a.chat.Edit(ctx, crewID, messageID, auth.MustUser(ctx).ID, req.Body)
	if err != nil {
		return postError(c, err, "The message could not be changed.")
	}
	return c.JSON(http.StatusOK, projectMessage(ctx, m))
}

func (a *API) deleteMessage(c *echo.Context) error {
	crewID, messageID, err := twoIDs(c, "messageID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.chat.Delete(ctx, crewID, messageID, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, "The message could not be deleted.")
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) react(on bool) echo.HandlerFunc {
	return func(c *echo.Context) error {
		crewID, messageID, err := twoIDs(c, "messageID")
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		if err := a.chat.React(ctx, crewID, messageID, auth.MustUser(ctx).ID, c.Param("emoji"), on); err != nil {
			return httpx.Fail(err, "The reaction could not be saved.")
		}
		return c.NoContent(http.StatusNoContent)
	}
}

func (a *API) pin(on bool) echo.HandlerFunc {
	return func(c *echo.Context) error {
		crewID, messageID, err := twoIDs(c, "messageID")
		if err != nil {
			return err
		}
		ctx := c.Request().Context()
		if err := a.chat.Pin(ctx, crewID, messageID, auth.MustUser(ctx).ID, on); err != nil {
			return httpx.Fail(err, "The pin could not be changed.")
		}
		return c.NoContent(http.StatusNoContent)
	}
}

func (a *API) report(c *echo.Context) error {
	crewID, messageID, err := twoIDs(c, "messageID")
	if err != nil {
		return err
	}
	var req ReportRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.chat.Report(ctx, crewID, messageID, auth.MustUser(ctx).ID, req.Reason, req.Note); err != nil {
		return httpx.Fail(err, "The report could not be sent.")
	}
	return c.NoContent(http.StatusNoContent)
}

// unread is, per crew, what the caller has not read, for the badge on the
// way in.
func (a *API) unread(c *echo.Context) error {
	ctx := c.Request().Context()
	list, err := a.chat.Unread(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "Unread counts could not be loaded.")
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
	return c.JSON(http.StatusOK, out)
}

// unreadByCrew sums the caller's unread channels per crew. Without a chat
// (an API built for tests of the board alone) there is nothing unread.
func (a *API) unreadByCrew(ctx context.Context) (map[uuid.UUID]CrewUnreadView, error) {
	out := map[uuid.UUID]CrewUnreadView{}
	if a.chat == nil {
		return out, nil
	}
	list, err := a.chat.Unread(ctx, auth.MustUser(ctx).ID)
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
func postError(c *echo.Context, err error, message string) error {
	switch {
	case errors.Is(err, moderation.ErrTermsRequired):
		return c.JSON(http.StatusForbidden, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: "Accept the community terms to post.", Code: "terms_required"}})
	case errors.Is(err, moderation.ErrSuspended):
		return c.JSON(http.StatusForbidden, httpx.ErrorBody{Error: httpx.ErrorDetail{Message: "You can't post right now.", Code: "suspended"}})
	default:
		return httpx.Fail(err, message)
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
func twoIDs(c *echo.Context, name string) (uuid.UUID, uuid.UUID, error) {
	crewID, err := echox.UUIDParam(c, "crewID")
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	id, err := echox.UUIDParam(c, name)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return crewID, id, nil
}

// queryInt reads an optional whole number from the query; empty is zero.
func queryInt(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}
