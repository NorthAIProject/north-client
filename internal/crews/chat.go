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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/crews/crew"
	crewsdb "github.com/NorthAIProject/north-client/internal/crews/db"
	"github.com/NorthAIProject/north-client/internal/crews/live"
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
	pool   *pgxpool.Pool
	q      *crewsdb.Queries
	mod    *moderation.Service
	filter *moderation.Filter
	queue  *jobs.Queue
	// sends bounds how fast one person can post. In memory, because the web
	// runs one replica; a second replica would double the allowance.
	sends   *ratelimit.Limiters
	pushers []ThreadSender
	log     *slog.Logger
	now     func() time.Time
}

func NewChat(pool *pgxpool.Pool, mod *moderation.Service, filter *moderation.Filter, queue *jobs.Queue) *Chat {
	return &Chat{
		pool: pool, q: crewsdb.New(pool), mod: mod, filter: filter, queue: queue,
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
	if !c.sends.Allow(authorID.String()) {
		return Message{}, apperr.FieldErrors{}.Add("body", i18n.T(ctx, "crews.err.slow"))
	}
	if in.ReplyToID != nil {
		target, tErr := c.q.MessageForViewer(ctx, crewsdb.MessageForViewerParams{Viewer: authorID, ID: *in.ReplyToID, CrewID: crewID})
		if tErr != nil && !errors.Is(tErr, pgx.ErrNoRows) {
			return Message{}, apperr.Wrap(tErr, "reply target")
		}
		if tErr != nil || target.ChannelID != channelID || target.DeletedAt != nil {
			return Message{}, apperr.FieldErrors{}.Add("reply", i18n.T(ctx, "crews.err.reply"))
		}
	}
	handles, all := parseMentions(body)
	mentions := []uuid.UUID{}
	if len(handles) > 0 {
		if mentions, err = c.q.MemberIDsByHandle(ctx, crewsdb.MemberIDsByHandleParams{CrewID: crewID, Handles: handles}); err != nil {
			return Message{}, apperr.Wrap(err, "resolve mentions")
		}
	}

	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return Message{}, apperr.Wrap(err, "begin send")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := c.q.WithTx(tx)
	if err = q.LockChannelForWrite(ctx, channelID); err != nil {
		return Message{}, apperr.Wrap(err, "lock channel")
	}
	row, err := q.InsertMessage(ctx, crewsdb.InsertMessageParams{
		CrewID: crewID, ChannelID: channelID, AuthorID: &authorID, Body: body,
		ReplyToID: in.ReplyToID, Mentions: mentions, MentionAll: all && role.CanManage(), ClientID: in.ClientID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Sent before: hand back the first one.
		id, idErr := q.MessageByClientID(ctx, crewsdb.MessageByClientIDParams{AuthorID: &authorID, ClientID: in.ClientID})
		if idErr != nil {
			return Message{}, apperr.Wrap(idErr, "resent message")
		}
		return c.Get(ctx, crewID, id, authorID)
	}
	if err != nil {
		return Message{}, apperr.Wrap(err, "insert message")
	}
	if err = live.Notify(ctx, tx, live.Event{Type: live.EventMessage, CrewID: crewID, ChannelID: channelID, MessageID: row.ID, Seq: row.Seq}); err != nil {
		return Message{}, err
	}
	if _, err = c.queue.EnqueueTx(ctx, tx, jobs.KindCrewMessagePush, jobs.CrewMessagePushPayload{UserID: authorID, MessageID: row.ID}); err != nil {
		return Message{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Message{}, apperr.Wrap(err, "commit send")
	}
	return c.Get(ctx, crewID, row.ID, authorID)
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

	var rows []crewsdb.MessagesAfterRow
	var page Page
	if after > 0 {
		got, err := c.q.MessagesAfter(ctx, crewsdb.MessagesAfterParams{Viewer: viewerID, ChannelID: channelID, After: after, Lim: int32(limit)})
		if err != nil {
			return Page{}, apperr.Wrap(err, "messages after")
		}
		rows = got
	} else {
		if before <= 0 {
			before = math.MaxInt64
		}
		got, err := c.q.MessagesBefore(ctx, crewsdb.MessagesBeforeParams{Viewer: viewerID, ChannelID: channelID, Before: before, Lim: int32(limit + 1)})
		if err != nil {
			return Page{}, apperr.Wrap(err, "messages before")
		}
		if len(got) > limit {
			page.HasMore = true
			got = got[:limit]
		}
		for i := len(got) - 1; i >= 0; i-- {
			rows = append(rows, crewsdb.MessagesAfterRow(got[i]))
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
	row, err := c.q.MessageForViewer(ctx, crewsdb.MessageForViewerParams{Viewer: viewerID, ID: messageID, CrewID: crewID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, apperr.ErrNotFound
	}
	if err != nil {
		return Message{}, apperr.Wrap(err, "get message")
	}
	msgs, err := c.messages(ctx, viewerID, []crewsdb.MessagesAfterRow{crewsdb.MessagesAfterRow(row)})
	if err != nil {
		return Message{}, err
	}
	return msgs[0], nil
}

// messages turns rows into messages with their reactions, as viewerID sees
// them.
func (c *Chat) messages(ctx context.Context, viewerID uuid.UUID, rows []crewsdb.MessagesAfterRow) ([]Message, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	reactions := map[uuid.UUID][]Reaction{}
	if len(ids) > 0 {
		got, err := c.q.ReactionsFor(ctx, crewsdb.ReactionsForParams{Viewer: viewerID, Ids: ids})
		if err != nil {
			return nil, apperr.Wrap(err, "reactions")
		}
		for _, r := range got {
			reactions[r.MessageID] = append(reactions[r.MessageID], Reaction{Emoji: r.Emoji, Count: int(r.N), Mine: r.Mine})
		}
	}
	out := make([]Message, 0, len(rows))
	for _, r := range rows {
		out = append(out, toMessage(r, viewerID, reactions[r.ID]))
	}
	return out, nil
}

func toMessage(r crewsdb.MessagesAfterRow, viewerID uuid.UUID, reactions []Reaction) Message {
	slices.SortFunc(reactions, func(a, b Reaction) int { return emojiOrder(a.Emoji) - emojiOrder(b.Emoji) })
	m := Message{
		ID: r.ID, Seq: r.Seq, ChannelID: r.ChannelID, Kind: r.Kind, Body: r.Body,
		Mentions: r.Mentions, MentionsMe: r.MentionAll || slices.Contains(r.Mentions, viewerID),
		Reactions: reactions, Pinned: r.PinnedAt != nil, Edited: r.EditedAt != nil,
		Deleted: r.DeletedAt != nil, CreatedAt: r.CreatedAt,
	}
	if r.AuthorID != nil {
		m.Author = &Person{ID: *r.AuthorID, Name: deref(r.AuthorName), Handle: deref(r.AuthorHandle)}
	}
	if r.ReplyID != nil {
		q := &Quote{ID: *r.ReplyID, Unavailable: r.ReplyUnavailable}
		if !q.Unavailable {
			q.AuthorName = deref(r.ReplyAuthorName)
			q.Excerpt = excerpt(deref(r.ReplyBody), 120)
		}
		m.ReplyTo = q
	}
	return m
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

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// member is the crew and what userID is in it; not found when they are not
// in it.
func (c *Chat) member(ctx context.Context, crewID, userID uuid.UUID) (crewsdb.Crew, Role, error) {
	cr, err := c.q.GetCrew(ctx, crewID)
	if errors.Is(err, pgx.ErrNoRows) {
		return crewsdb.Crew{}, "", apperr.ErrNotFound
	}
	if err != nil {
		return crewsdb.Crew{}, "", apperr.Wrap(err, "get crew")
	}
	if cr.OwnerID == userID {
		return cr, crew.RoleOwner, nil
	}
	r, err := c.q.MemberRole(ctx, crewsdb.MemberRoleParams{CrewID: crewID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return crewsdb.Crew{}, "", apperr.ErrNotFound
	}
	if err != nil {
		return crewsdb.Crew{}, "", apperr.Wrap(err, "member role")
	}
	return cr, Role(r), nil
}

// textChannel is nil when channelID is a text channel of crewID.
func (c *Chat) textChannel(ctx context.Context, crewID, channelID uuid.UUID) error {
	ch, err := c.q.ChannelInCrew(ctx, crewsdb.ChannelInCrewParams{ID: channelID, CrewID: crewID})
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.ErrNotFound
	}
	if err != nil {
		return apperr.Wrap(err, "channel")
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
