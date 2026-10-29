package crews

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/crews/crew"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/moderation"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/ratelimit"
)

type (
	Message  = crew.Message
	Page     = crew.Page
	Person   = crew.Person
	Quote    = crew.Quote
	Reaction = crew.Reaction
)

const (
	MessageKindText   = crew.MessageKindText
	MessageKindSystem = crew.MessageKindSystem
)

// SendInput is what a member posts. ClientID makes a resend after a lost
// response safe: the same author and ClientID is the same message.
type SendInput struct {
	Body      string
	ReplyToID *uuid.UUID
	ClientID  *uuid.UUID
}

// Chat is the crew's conversation: messages in text channels, as each member
// is allowed to see them.
type Chat struct {
	repo   ChatRepository
	mod    *moderation.Service
	filter *moderation.Filter
	queue  *jobs.Queue
	// sends bounds how fast one person can post. In memory, because the web
	// runs one replica; a second replica would double the allowance.
	sends   *ratelimit.Store
	pushers []ThreadSender
	log     *slog.Logger
	now     func() time.Time
}

// ChatRepository is the persistence Chat depends on: crews and channels as
// a member reaches them, messages with their reactions and pins, read marks,
// and what pushes and moderation read. Every write that watchers should see
// tells them in the same transaction. NewRepository is what the process
// wires, and a test can hand Chat anything that keeps messages.
type ChatRepository interface {
	GetCrew(ctx context.Context, crewID uuid.UUID) (Crew, error)
	MemberRole(ctx context.Context, crewID, userID uuid.UUID) (Role, error)
	ChannelInCrew(ctx context.Context, crewID, channelID uuid.UUID) (Channel, error)
	Unread(ctx context.Context, userID uuid.UUID) ([]Unread, error)
	MemberIDsByHandle(ctx context.Context, crewID uuid.UUID, handles []string) ([]uuid.UUID, error)
	VisibleMessage(ctx context.Context, viewerID, messageID, crewID uuid.UUID) (Message, error)
	MessagesAfter(ctx context.Context, viewerID, channelID uuid.UUID, after int64, limit int) ([]Message, error)
	MessagesBefore(ctx context.Context, viewerID, channelID uuid.UUID, before int64, limit int) ([]Message, error)
	Reactions(ctx context.Context, viewerID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]Reaction, error)
	InsertMessage(ctx context.Context, m newMessage, queue *jobs.Queue) (uuid.UUID, error)
	MessageMeta(ctx context.Context, crewID, messageID uuid.UUID) (messageMeta, error)
	EditMessage(ctx context.Context, meta messageMeta, body string, mentions []uuid.UUID) error
	DeleteMessage(ctx context.Context, meta messageMeta, actorID uuid.UUID) error
	SetReaction(ctx context.Context, meta messageMeta, userID uuid.UUID, emoji string, on bool) error
	SetPin(ctx context.Context, meta messageMeta, actorID uuid.UUID, on bool) error
	CountPins(ctx context.Context, channelID uuid.UUID) (int, error)
	PinnedIDs(ctx context.Context, channelID uuid.UUID) ([]uuid.UUID, error)
	MarkRead(ctx context.Context, channelID, userID uuid.UUID, seq int64) error
	SetHidden(ctx context.Context, messageID uuid.UUID, hide bool) error
	MessageCrewSize(ctx context.Context, messageID uuid.UUID) (int, error)
	MessageBody(ctx context.Context, messageID uuid.UUID) (string, error)
	PushMessage(ctx context.Context, messageID uuid.UUID) (pushMessage, error)
	PushRecipients(ctx context.Context, messageID uuid.UUID) ([]uuid.UUID, error)
	AuthorBlockedFor(ctx context.Context, viewerID, messageID uuid.UUID) (bool, error)
}

func NewChat(repo ChatRepository, mod *moderation.Service, filter *moderation.Filter, queue *jobs.Queue) *Chat {
	return &Chat{
		repo: repo, mod: mod, filter: filter, queue: queue,
		sends: ratelimit.New(crew.SendsPerMinute), log: slog.Default(), now: time.Now,
	}
}

// Send posts a message in a text channel and tells everyone watching.
func (c *Chat) Send(ctx context.Context, crewID, channelID, authorID uuid.UUID, in SendInput) (Message, error) {
	_, role, err := c.member(ctx, crewID, authorID)
	if err != nil {
		return Message{}, err
	}
	if err = c.textChannel(ctx, crewID, channelID); err != nil {
		return Message{}, err
	}
	body, err := c.validBody(ctx, in.Body)
	if err != nil {
		return Message{}, err
	}
	if err = c.mod.CanPost(ctx, authorID); err != nil {
		return Message{}, err
	}
	if ok, _ := c.sends.Allow(authorID.String()); !ok {
		return Message{}, apperr.FieldErrors{}.Add("body", i18n.T(ctx, "crews.err.slow"))
	}
	if in.ReplyToID != nil {
		target, tErr := c.repo.VisibleMessage(ctx, authorID, *in.ReplyToID, crewID)
		if tErr != nil && !errors.Is(tErr, apperr.ErrNotFound) {
			return Message{}, apperr.Wrap(tErr, "reply target")
		}
		if tErr != nil || target.ChannelID != channelID || target.Deleted {
			return Message{}, apperr.FieldErrors{}.Add("reply", i18n.T(ctx, "crews.err.reply"))
		}
	}
	handles, all := parseMentions(body)
	mentions := []uuid.UUID{}
	if len(handles) > 0 {
		if mentions, err = c.repo.MemberIDsByHandle(ctx, crewID, handles); err != nil {
			return Message{}, err
		}
	}

	id, err := c.repo.InsertMessage(ctx, newMessage{
		CrewID: crewID, ChannelID: channelID, AuthorID: authorID, Body: body,
		ReplyToID: in.ReplyToID, Mentions: mentions, MentionAll: all && role.CanManage(), ClientID: in.ClientID,
	}, c.queue)
	if err != nil {
		return Message{}, err
	}
	return c.Get(ctx, crewID, id, authorID)
}

// List is a page of a text channel as viewerID sees it, oldest first.
// before and after are seq cursors, exclusive, and at most one is set:
// after catches up on newer messages, before pages back through older ones,
// and neither gives the newest page.
func (c *Chat) List(ctx context.Context, crewID, channelID, viewerID uuid.UUID, before, after int64, limit int) (Page, error) {
	if _, _, err := c.member(ctx, crewID, viewerID); err != nil {
		return Page{}, err
	}
	if err := c.textChannel(ctx, crewID, channelID); err != nil {
		return Page{}, err
	}
	if limit <= 0 {
		limit = crew.PageSize
	}
	limit = min(limit, crew.MaxPage)

	var rows []Message
	var page Page
	if after > 0 {
		got, err := c.repo.MessagesAfter(ctx, viewerID, channelID, after, limit)
		if err != nil {
			return Page{}, err
		}
		rows = got
	} else {
		if before <= 0 {
			before = math.MaxInt64
		}
		got, err := c.repo.MessagesBefore(ctx, viewerID, channelID, before, limit+1)
		if err != nil {
			return Page{}, err
		}
		if len(got) > limit {
			page.HasMore = true
			got = got[:limit]
		}
		for i := len(got) - 1; i >= 0; i-- {
			rows = append(rows, got[i])
		}
	}
	msgs, err := c.messages(ctx, viewerID, rows)
	if err != nil {
		return Page{}, err
	}
	page.Messages = msgs
	return page, nil
}

// Get is one message as viewerID sees it; not found when they may not.
func (c *Chat) Get(ctx context.Context, crewID, messageID, viewerID uuid.UUID) (Message, error) {
	if _, _, err := c.member(ctx, crewID, viewerID); err != nil {
		return Message{}, err
	}
	m, err := c.repo.VisibleMessage(ctx, viewerID, messageID, crewID)
	if errors.Is(err, apperr.ErrNotFound) {
		return Message{}, err
	}
	if err != nil {
		return Message{}, apperr.Wrap(err, "get message")
	}
	msgs, err := c.messages(ctx, viewerID, []Message{m})
	if err != nil {
		return Message{}, err
	}
	return msgs[0], nil
}

// messages adds their reactions to messages, as viewerID sees them.
func (c *Chat) messages(ctx context.Context, viewerID uuid.UUID, msgs []Message) ([]Message, error) {
	ids := make([]uuid.UUID, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	reactions := map[uuid.UUID][]Reaction{}
	if len(ids) > 0 {
		got, err := c.repo.Reactions(ctx, viewerID, ids)
		if err != nil {
			return nil, err
		}
		reactions = got
	}
	out := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		m.Reactions = reactions[m.ID]
		slices.SortFunc(m.Reactions, func(a, b Reaction) int { return emojiOrder(a.Emoji) - emojiOrder(b.Emoji) })
		out = append(out, m)
	}
	return out, nil
}

func emojiOrder(key string) int {
	return slices.IndexFunc(crew.ReactionEmoji, func(e crew.Emoji) bool { return e.Key == key })
}

func excerpt(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// member is the crew and what userID is in it; not found when they are not
// in it.
func (c *Chat) member(ctx context.Context, crewID, userID uuid.UUID) (Crew, Role, error) {
	cr, err := c.repo.GetCrew(ctx, crewID)
	if err != nil {
		return Crew{}, "", err
	}
	if cr.OwnerID == userID {
		return cr, crew.RoleOwner, nil
	}
	r, err := c.repo.MemberRole(ctx, crewID, userID)
	if err != nil {
		return Crew{}, "", err
	}
	return cr, r, nil
}

// textChannel is nil when channelID is a text channel of crewID.
func (c *Chat) textChannel(ctx context.Context, crewID, channelID uuid.UUID) error {
	ch, err := c.repo.ChannelInCrew(ctx, crewID, channelID)
	if err != nil {
		return err
	}
	if ch.Kind != crew.ChannelText {
		return apperr.ErrNotFound
	}
	return nil
}

// validBody trims body and checks its length and the word filter.
func (c *Chat) validBody(ctx context.Context, body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > crew.MaxBody {
		return "", apperr.FieldErrors{}.Add("body", i18n.T(ctx, "crews.err.body"))
	}
	if errors.Is(c.filter.Check(body), moderation.ErrBlockedContent) {
		return "", apperr.FieldErrors{}.Add("body", i18n.T(ctx, "crews.err.blocked"))
	}
	return body, nil
}
