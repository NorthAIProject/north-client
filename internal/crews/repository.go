package crews

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/crews/crew"
	crewsdb "github.com/NorthAIProject/north-client/internal/crews/db"
	"github.com/NorthAIProject/north-client/internal/crews/live"
	"github.com/NorthAIProject/north-client/internal/jobs"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// PostgresRepository keeps crews, their members, channels and messages in
// Postgres, and tells live streams about changes through NOTIFY in the same
// transaction as the write. It is the Repository the process wires into
// Service and the ChatRepository it wires into Chat.
type PostgresRepository struct {
	pool *pgxpool.Pool
	q    *crewsdb.Queries
}

var (
	_ Repository     = (*PostgresRepository)(nil)
	_ ChatRepository = (*PostgresRepository)(nil)
)

func NewRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, q: crewsdb.New(pool)}
}

// errChannelNameTaken is a channel name already used in the crew.
var errChannelNameTaken = errors.New("channel name taken")

// memberRecord is one member of a crew as stored.
type memberRecord struct {
	ID          uuid.UUID
	DisplayName string
	Handle      *string
	Timezone    string
	Tier        string
	Role        Role
}

// crewmate is somebody sharing a crew with a person.
type crewmate struct {
	ID          uuid.UUID
	DisplayName string
	Timezone    string
}

// crewChallenge is a weekly challenge of one of a person's crews.
type crewChallenge struct {
	CrewID    uuid.UUID
	CrewName  string
	Kind      string
	Target    int
	CreatedAt time.Time
}

// messageMeta is what an action on a message needs to know about it.
type messageMeta struct {
	ID        uuid.UUID
	CrewID    uuid.UUID
	ChannelID uuid.UUID
	AuthorID  *uuid.UUID
	DeletedAt *time.Time
	PinnedAt  *time.Time
}

// newMessage is a member's message to store.
type newMessage struct {
	CrewID     uuid.UUID
	ChannelID  uuid.UUID
	AuthorID   uuid.UUID
	Body       string
	ReplyToID  *uuid.UUID
	Mentions   []uuid.UUID
	MentionAll bool
	ClientID   *uuid.UUID
}

// pushMessage is a message as its push notification reads it.
type pushMessage struct {
	CrewID      uuid.UUID
	ChannelID   uuid.UUID
	Body        string
	CrewName    string
	ChannelName string
	AuthorName  string
}

// Notify tells anyone watching the crew about ev, outside any write.
func (r *PostgresRepository) Notify(ctx context.Context, ev live.Event) error {
	return live.Notify(ctx, r.pool, ev)
}

// CreateCrew makes a crew with its owner as first member, its default
// channels and the welcome message, in one transaction.
func (r *PostgresRepository) CreateCrew(ctx context.Context, ownerID uuid.UUID, name, code string) (Crew, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Crew{}, apperr.Wrap(err, "begin create crew")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)
	row, err := q.CreateCrew(ctx, crewsdb.CreateCrewParams{Name: name, OwnerID: ownerID, Code: code})
	if err != nil {
		return Crew{}, apperr.Wrap(err, "create crew")
	}
	if _, err = q.AddMember(ctx, crewsdb.AddMemberParams{CrewID: row.ID, UserID: ownerID}); err != nil {
		return Crew{}, apperr.Wrap(err, "add owner")
	}
	if err = q.CreateDefaultChannels(ctx, row.ID); err != nil {
		return Crew{}, apperr.Wrap(err, "create default channels")
	}
	general, err := q.DefaultChannel(ctx, row.ID)
	if err != nil {
		return Crew{}, apperr.Wrap(err, "default channel")
	}
	if err = q.InsertSystemMessage(ctx, crewsdb.InsertSystemMessageParams{CrewID: row.ID, ChannelID: general, Body: "crews.sys.welcome"}); err != nil {
		return Crew{}, apperr.Wrap(err, "welcome message")
	}
	if err = tx.Commit(ctx); err != nil {
		return Crew{}, apperr.Wrap(err, "commit create crew")
	}
	return toCrew(row), nil
}

// CrewByCode is the crew behind a join code, or apperr.ErrNotFound.
func (r *PostgresRepository) CrewByCode(ctx context.Context, code string) (Crew, error) {
	row, err := r.q.CrewByCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return Crew{}, apperr.ErrNotFound
	}
	if err != nil {
		return Crew{}, apperr.Wrap(err, "crew by code")
	}
	return toCrew(row), nil
}

// GetCrew is one crew, or apperr.ErrNotFound. Members is not counted.
func (r *PostgresRepository) GetCrew(ctx context.Context, crewID uuid.UUID) (Crew, error) {
	row, err := r.q.GetCrew(ctx, crewID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Crew{}, apperr.ErrNotFound
	}
	if err != nil {
		return Crew{}, apperr.Wrap(err, "get crew")
	}
	return toCrew(row), nil
}

func (r *PostgresRepository) CountMembers(ctx context.Context, crewID uuid.UUID) (int, error) {
	n, err := r.q.CountMembers(ctx, crewID)
	if err != nil {
		return 0, apperr.Wrap(err, "count members")
	}
	return int(n), nil
}

func (r *PostgresRepository) IsMember(ctx context.Context, crewID, userID uuid.UUID) (bool, error) {
	member, err := r.q.IsMember(ctx, crewsdb.IsMemberParams{CrewID: crewID, UserID: userID})
	if err != nil {
		return false, apperr.Wrap(err, "check membership")
	}
	return member, nil
}

// AddMember adds a person to a crew; adding them twice is a no-op.
func (r *PostgresRepository) AddMember(ctx context.Context, crewID, userID uuid.UUID) error {
	if _, err := r.q.AddMember(ctx, crewsdb.AddMemberParams{CrewID: crewID, UserID: userID}); err != nil {
		return apperr.Wrap(err, "join crew")
	}
	return nil
}

// Members is everyone in the crew, longest-standing first.
func (r *PostgresRepository) Members(ctx context.Context, crewID uuid.UUID) ([]memberRecord, error) {
	rows, err := r.q.Members(ctx, crewID)
	if err != nil {
		return nil, apperr.Wrap(err, "list members")
	}
	out := make([]memberRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, memberRecord{
			ID: row.ID, DisplayName: row.DisplayName, Handle: row.Handle, Timezone: row.Timezone,
			Tier: row.Tier, Role: Role(row.Role),
		})
	}
	return out, nil
}

// OldestOtherMember is whoever has been in the crew longest apart from
// userID, or apperr.ErrNotFound when nobody else is in it.
func (r *PostgresRepository) OldestOtherMember(ctx context.Context, crewID, userID uuid.UUID) (uuid.UUID, error) {
	next, err := r.q.OldestOtherMember(ctx, crewsdb.OldestOtherMemberParams{CrewID: crewID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apperr.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, apperr.Wrap(err, "find next owner")
	}
	return next, nil
}

func (r *PostgresRepository) DeleteCrew(ctx context.Context, crewID uuid.UUID) error {
	if err := r.q.DeleteCrew(ctx, crewID); err != nil {
		return apperr.Wrap(err, "delete crew")
	}
	return nil
}

func (r *PostgresRepository) SetOwner(ctx context.Context, crewID, ownerID uuid.UUID) error {
	if err := r.q.SetOwner(ctx, crewsdb.SetOwnerParams{ID: crewID, OwnerID: ownerID}); err != nil {
		return apperr.Wrap(err, "hand over crew")
	}
	return nil
}

func (r *PostgresRepository) RemoveMember(ctx context.Context, crewID, userID uuid.UUID) error {
	if _, err := r.q.RemoveMember(ctx, crewsdb.RemoveMemberParams{CrewID: crewID, UserID: userID}); err != nil {
		return apperr.Wrap(err, "remove member")
	}
	return nil
}

// SetMemberRole changes a member's role; apperr.ErrNotFound when they are
// not in the crew.
func (r *PostgresRepository) SetMemberRole(ctx context.Context, crewID, userID uuid.UUID, role Role) error {
	n, err := r.q.SetMemberRole(ctx, crewsdb.SetMemberRoleParams{CrewID: crewID, UserID: userID, Role: string(role)})
	if err != nil {
		return apperr.Wrap(err, "set role")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// MemberRole is a member's stored role, or apperr.ErrNotFound when they are
// not in the crew.
func (r *PostgresRepository) MemberRole(ctx context.Context, crewID, userID uuid.UUID) (Role, error) {
	role, err := r.q.MemberRole(ctx, crewsdb.MemberRoleParams{CrewID: crewID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperr.ErrNotFound
	}
	if err != nil {
		return "", apperr.Wrap(err, "member role")
	}
	return Role(role), nil
}

// MemberSettings is a member's push setting and mute.
func (r *PostgresRepository) MemberSettings(ctx context.Context, crewID, userID uuid.UUID) (string, *time.Time, error) {
	settings, err := r.q.MemberSettings(ctx, crewsdb.MemberSettingsParams{CrewID: crewID, UserID: userID})
	if err != nil {
		return "", nil, apperr.Wrap(err, "member settings")
	}
	return settings.Notify, settings.MutedUntil, nil
}

// SetNotify stores a member's push setting; apperr.ErrNotFound when they are
// not in the crew.
func (r *PostgresRepository) SetNotify(ctx context.Context, crewID, userID uuid.UUID, notify string, mutedUntil *time.Time) error {
	n, err := r.q.SetNotify(ctx, crewsdb.SetNotifyParams{CrewID: crewID, UserID: userID, Notify: notify, MutedUntil: mutedUntil})
	if err != nil {
		return apperr.Wrap(err, "set notify")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) ListMine(ctx context.Context, userID uuid.UUID) ([]Crew, error) {
	rows, err := r.q.ListMine(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list crews")
	}
	out := make([]Crew, 0, len(rows))
	for _, row := range rows {
		out = append(out, Crew{ID: row.ID, Name: row.Name, OwnerID: row.OwnerID, Code: row.Code, Members: int(row.Members)})
	}
	return out, nil
}

// CrewmatesOf is everybody sharing a crew with userID.
func (r *PostgresRepository) CrewmatesOf(ctx context.Context, userID uuid.UUID) ([]crewmate, error) {
	rows, err := r.q.CrewmatesOf(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "crewmates")
	}
	out := make([]crewmate, 0, len(rows))
	for _, row := range rows {
		out = append(out, crewmate{ID: row.ID, DisplayName: row.DisplayName, Timezone: row.Timezone})
	}
	return out, nil
}

// Challenge is the crew's weekly challenge, nil when there is none.
func (r *PostgresRepository) Challenge(ctx context.Context, crewID uuid.UUID) (*Challenge, error) {
	ch, err := r.q.GetChallenge(ctx, crewID)
	switch {
	case err == nil:
		return &Challenge{Kind: ch.Kind, Target: int(ch.Target)}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, apperr.Wrap(err, "get challenge")
	}
	return nil, nil
}

func (r *PostgresRepository) ClearChallenge(ctx context.Context, crewID uuid.UUID) error {
	return r.q.ClearChallenge(ctx, crewID)
}

func (r *PostgresRepository) SetChallenge(ctx context.Context, crewID uuid.UUID, kind string, target int) error {
	return r.q.SetChallenge(ctx, crewsdb.SetChallengeParams{CrewID: crewID, Kind: kind, Target: int16(target)})
}

// ChallengesOf is the weekly challenge of every crew userID is in.
func (r *PostgresRepository) ChallengesOf(ctx context.Context, userID uuid.UUID) ([]crewChallenge, error) {
	rows, err := r.q.ChallengesOf(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "crew challenges")
	}
	out := make([]crewChallenge, 0, len(rows))
	for _, row := range rows {
		out = append(out, crewChallenge{CrewID: row.ID, CrewName: row.Name, Kind: row.Kind, Target: int(row.Target), CreatedAt: row.CreatedAt})
	}
	return out, nil
}

// ListChannels is the crew's channels in order.
func (r *PostgresRepository) ListChannels(ctx context.Context, crewID uuid.UUID) ([]Channel, error) {
	rows, err := r.q.ListChannels(ctx, crewID)
	if err != nil {
		return nil, apperr.Wrap(err, "list channels")
	}
	out := make([]Channel, 0, len(rows))
	for _, row := range rows {
		out = append(out, toChannel(row))
	}
	return out, nil
}

// ChannelInCrew is one of the crew's channels, of any kind, or
// apperr.ErrNotFound.
func (r *PostgresRepository) ChannelInCrew(ctx context.Context, crewID, channelID uuid.UUID) (Channel, error) {
	ch, err := r.q.ChannelInCrew(ctx, crewsdb.ChannelInCrewParams{ID: channelID, CrewID: crewID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Channel{}, apperr.ErrNotFound
	}
	if err != nil {
		return Channel{}, apperr.Wrap(err, "channel")
	}
	return toChannel(ch), nil
}

func (r *PostgresRepository) CountChannels(ctx context.Context, crewID uuid.UUID) (int, error) {
	n, err := r.q.CountChannels(ctx, crewID)
	if err != nil {
		return 0, apperr.Wrap(err, "count channels")
	}
	return int(n), nil
}

// CreateChannel adds a text channel and tells watchers, in one transaction.
// A name already in the crew is errChannelNameTaken.
func (r *PostgresRepository) CreateChannel(ctx context.Context, crewID, createdBy uuid.UUID, name, topic string) (Channel, error) {
	var row crewsdb.CrewChannel
	err := r.inTx(ctx, func(q *crewsdb.Queries, tx pgx.Tx) error {
		var insErr error
		if row, insErr = q.InsertChannel(ctx, crewsdb.InsertChannelParams{CrewID: crewID, Name: name, Topic: topic, CreatedBy: &createdBy}); insErr != nil {
			return insErr
		}
		return live.Notify(ctx, tx, live.Event{Type: live.EventChannels, CrewID: crewID})
	})
	if err != nil {
		return Channel{}, nameTaken(err)
	}
	return toChannel(row), nil
}

// UpdateChannel renames a channel or changes its topic and tells watchers,
// in one transaction. A name already in the crew is errChannelNameTaken.
func (r *PostgresRepository) UpdateChannel(ctx context.Context, crewID, channelID uuid.UUID, name, topic string) (Channel, error) {
	var row crewsdb.CrewChannel
	err := r.inTx(ctx, func(q *crewsdb.Queries, tx pgx.Tx) error {
		var upErr error
		if row, upErr = q.UpdateChannel(ctx, crewsdb.UpdateChannelParams{ID: channelID, Name: name, Topic: topic}); upErr != nil {
			return upErr
		}
		return live.Notify(ctx, tx, live.Event{Type: live.EventChannels, CrewID: crewID})
	})
	if err != nil {
		return Channel{}, nameTaken(err)
	}
	return toChannel(row), nil
}

// DeleteChannel removes a channel and its messages and tells watchers, in
// one transaction.
func (r *PostgresRepository) DeleteChannel(ctx context.Context, crewID, channelID uuid.UUID) error {
	return r.inTx(ctx, func(q *crewsdb.Queries, tx pgx.Tx) error {
		if delErr := q.DeleteChannel(ctx, channelID); delErr != nil {
			return apperr.Wrap(delErr, "delete channel")
		}
		return live.Notify(ctx, tx, live.Event{Type: live.EventChannels, CrewID: crewID})
	})
}

// MessagesAuthoredBy is every crew message userID posted and kept.
func (r *PostgresRepository) MessagesAuthoredBy(ctx context.Context, userID uuid.UUID) ([]crew.ExportedMessage, error) {
	rows, err := r.q.MessagesAuthoredBy(ctx, &userID)
	if err != nil {
		return nil, apperr.Wrap(err, "messages authored by")
	}
	out := make([]crew.ExportedMessage, 0, len(rows))
	for _, row := range rows {
		out = append(out, crew.ExportedMessage{Crew: row.CrewName, Channel: row.ChannelName, Body: row.Body, CreatedAt: row.CreatedAt, Edited: row.Edited})
	}
	return out, nil
}

// Unread is, for every text channel with something new for userID, how much
// and how much of it is for them.
func (r *PostgresRepository) Unread(ctx context.Context, userID uuid.UUID) ([]Unread, error) {
	rows, err := r.q.Unread(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "unread")
	}
	out := make([]Unread, 0, len(rows))
	for _, row := range rows {
		out = append(out, Unread{CrewID: row.CrewID, ChannelID: row.ChannelID, Count: int(row.Unread), Mentions: int(row.Mentions)})
	}
	return out, nil
}

// MemberIDsByHandle is the members of the crew with any of handles.
func (r *PostgresRepository) MemberIDsByHandle(ctx context.Context, crewID uuid.UUID, handles []string) ([]uuid.UUID, error) {
	ids, err := r.q.MemberIDsByHandle(ctx, crewsdb.MemberIDsByHandleParams{CrewID: crewID, Handles: handles})
	if err != nil {
		return nil, apperr.Wrap(err, "resolve mentions")
	}
	return ids, nil
}

// VisibleMessage is one message as viewerID may see it, without reactions.
// apperr.ErrNotFound when they may not; other errors are not wrapped.
func (r *PostgresRepository) VisibleMessage(ctx context.Context, viewerID, messageID, crewID uuid.UUID) (Message, error) {
	row, err := r.q.MessageForViewer(ctx, crewsdb.MessageForViewerParams{Viewer: viewerID, ID: messageID, CrewID: crewID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, apperr.ErrNotFound
	}
	if err != nil {
		return Message{}, err
	}
	return toMessage(crewsdb.MessagesAfterRow(row), viewerID), nil
}

// MessagesAfter is up to limit messages after seq after, oldest first, as
// viewerID sees them, without reactions.
func (r *PostgresRepository) MessagesAfter(ctx context.Context, viewerID, channelID uuid.UUID, after int64, limit int) ([]Message, error) {
	rows, err := r.q.MessagesAfter(ctx, crewsdb.MessagesAfterParams{Viewer: viewerID, ChannelID: channelID, After: after, Lim: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "messages after")
	}
	out := make([]Message, 0, len(rows))
	for _, row := range rows {
		out = append(out, toMessage(row, viewerID))
	}
	return out, nil
}

// MessagesBefore is up to limit messages before seq before, newest first,
// as viewerID sees them, without reactions.
func (r *PostgresRepository) MessagesBefore(ctx context.Context, viewerID, channelID uuid.UUID, before int64, limit int) ([]Message, error) {
	rows, err := r.q.MessagesBefore(ctx, crewsdb.MessagesBeforeParams{Viewer: viewerID, ChannelID: channelID, Before: before, Lim: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "messages before")
	}
	out := make([]Message, 0, len(rows))
	for _, row := range rows {
		out = append(out, toMessage(crewsdb.MessagesAfterRow(row), viewerID))
	}
	return out, nil
}

// Reactions is the reactions on each of ids, as viewerID sees them, in no
// particular order.
func (r *PostgresRepository) Reactions(ctx context.Context, viewerID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]Reaction, error) {
	got, err := r.q.ReactionsFor(ctx, crewsdb.ReactionsForParams{Viewer: viewerID, Ids: ids})
	if err != nil {
		return nil, apperr.Wrap(err, "reactions")
	}
	reactions := map[uuid.UUID][]Reaction{}
	for _, row := range got {
		reactions[row.MessageID] = append(reactions[row.MessageID], Reaction{Emoji: row.Emoji, Count: int(row.N), Mine: row.Mine})
	}
	return reactions, nil
}

// InsertMessage stores a message under the channel's write lock, tells
// watchers and queues its push, in one transaction. A resend of a message
// already stored (same author and ClientID) stores nothing and is the first
// one's id.
func (r *PostgresRepository) InsertMessage(ctx context.Context, m newMessage, queue *jobs.Queue) (uuid.UUID, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, apperr.Wrap(err, "begin send")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)
	if err = q.LockChannelForWrite(ctx, m.ChannelID); err != nil {
		return uuid.Nil, apperr.Wrap(err, "lock channel")
	}
	row, err := q.InsertMessage(ctx, crewsdb.InsertMessageParams{
		CrewID: m.CrewID, ChannelID: m.ChannelID, AuthorID: &m.AuthorID, Body: m.Body,
		ReplyToID: m.ReplyToID, Mentions: m.Mentions, MentionAll: m.MentionAll, ClientID: m.ClientID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Sent before: hand back the first one.
		id, idErr := q.MessageByClientID(ctx, crewsdb.MessageByClientIDParams{AuthorID: &m.AuthorID, ClientID: m.ClientID})
		if idErr != nil {
			return uuid.Nil, apperr.Wrap(idErr, "resent message")
		}
		return id, nil
	}
	if err != nil {
		return uuid.Nil, apperr.Wrap(err, "insert message")
	}
	if err = live.Notify(ctx, tx, live.Event{Type: live.EventMessage, CrewID: m.CrewID, ChannelID: m.ChannelID, MessageID: row.ID, Seq: row.Seq}); err != nil {
		return uuid.Nil, err
	}
	if _, err = queue.EnqueueTx(ctx, tx, jobs.KindCrewMessagePush, jobs.CrewMessagePushPayload{UserID: m.AuthorID, MessageID: row.ID}); err != nil {
		return uuid.Nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return uuid.Nil, apperr.Wrap(err, "commit send")
	}
	return row.ID, nil
}

func (r *PostgresRepository) MessageMeta(ctx context.Context, crewID, messageID uuid.UUID) (messageMeta, error) {
	meta, err := r.q.MessageMeta(ctx, crewsdb.MessageMetaParams{ID: messageID, CrewID: crewID})
	if err != nil {
		return messageMeta{}, apperr.Wrap(err, "message meta")
	}
	return messageMeta{
		ID: meta.ID, CrewID: meta.CrewID, ChannelID: meta.ChannelID, AuthorID: meta.AuthorID,
		DeletedAt: meta.DeletedAt, PinnedAt: meta.PinnedAt,
	}, nil
}

// EditMessage changes a message's words and mentions and tells watchers.
func (r *PostgresRepository) EditMessage(ctx context.Context, meta messageMeta, body string, mentions []uuid.UUID) error {
	return r.update(ctx, meta, func(q *crewsdb.Queries) error {
		return q.EditMessage(ctx, crewsdb.EditMessageParams{ID: meta.ID, Body: body, Mentions: mentions})
	})
}

// DeleteMessage removes a message's words and reactions, keeping its place,
// and tells watchers.
func (r *PostgresRepository) DeleteMessage(ctx context.Context, meta messageMeta, actorID uuid.UUID) error {
	return r.update(ctx, meta, func(q *crewsdb.Queries) error {
		if err := q.DeleteMessage(ctx, crewsdb.DeleteMessageParams{ID: meta.ID, DeletedBy: &actorID}); err != nil {
			return err
		}
		return q.ClearReactions(ctx, meta.ID)
	})
}

// SetReaction adds (on) or takes back a reaction and tells watchers.
func (r *PostgresRepository) SetReaction(ctx context.Context, meta messageMeta, userID uuid.UUID, emoji string, on bool) error {
	return r.update(ctx, meta, func(q *crewsdb.Queries) error {
		if on {
			return q.AddReaction(ctx, crewsdb.AddReactionParams{MessageID: meta.ID, UserID: userID, Emoji: emoji})
		}
		return q.RemoveReaction(ctx, crewsdb.RemoveReactionParams{MessageID: meta.ID, UserID: userID, Emoji: emoji})
	})
}

// SetPin pins (on) or unpins a message and tells watchers.
func (r *PostgresRepository) SetPin(ctx context.Context, meta messageMeta, actorID uuid.UUID, on bool) error {
	return r.update(ctx, meta, func(q *crewsdb.Queries) error {
		return q.SetPin(ctx, crewsdb.SetPinParams{ID: meta.ID, Pin: on, PinnedBy: &actorID})
	})
}

func (r *PostgresRepository) CountPins(ctx context.Context, channelID uuid.UUID) (int, error) {
	n, err := r.q.CountPins(ctx, channelID)
	if err != nil {
		return 0, apperr.Wrap(err, "count pins")
	}
	return int(n), nil
}

// PinnedIDs is the channel's pinned messages, newest pin first.
func (r *PostgresRepository) PinnedIDs(ctx context.Context, channelID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := r.q.PinnedIDs(ctx, channelID)
	if err != nil {
		return nil, apperr.Wrap(err, "pinned ids")
	}
	return ids, nil
}

func (r *PostgresRepository) MarkRead(ctx context.Context, channelID, userID uuid.UUID, seq int64) error {
	if err := r.q.MarkRead(ctx, crewsdb.MarkReadParams{ChannelID: channelID, UserID: userID, LastReadSeq: seq}); err != nil {
		return apperr.Wrap(err, "mark read")
	}
	return nil
}

// SetHidden hides or unhides a message for the whole crew and tells
// watchers, in one transaction. A message already gone is not an error.
func (r *PostgresRepository) SetHidden(ctx context.Context, messageID uuid.UUID, hide bool) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return apperr.Wrap(err, "begin hide")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := r.q.WithTx(tx).SetHidden(ctx, crewsdb.SetHiddenParams{ID: messageID, Hide: hide})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // deleted with its channel or crew: nothing left to hide
	}
	if err != nil {
		return apperr.Wrap(err, "hide message")
	}
	if err = live.Notify(ctx, tx, live.Event{Type: live.EventUpdate, CrewID: row.CrewID, ChannelID: row.ChannelID, MessageID: row.ID}); err != nil {
		return err
	}
	return apperr.Wrap(tx.Commit(ctx), "commit hide")
}

// MessageCrewSize is how many people are in the crew of a message.
func (r *PostgresRepository) MessageCrewSize(ctx context.Context, messageID uuid.UUID) (int, error) {
	n, err := r.q.MessageCrewSize(ctx, messageID)
	if err != nil {
		return 0, apperr.Wrap(err, "crew size")
	}
	return int(n), nil
}

// MessageBody is a message's words; "" when it is gone.
func (r *PostgresRepository) MessageBody(ctx context.Context, messageID uuid.UUID) (string, error) {
	body, err := r.q.MessageBody(ctx, messageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", apperr.Wrap(err, "message body")
	}
	return body, nil
}

// PushMessage is a message to push, or apperr.ErrNotFound when it was
// deleted or hidden.
func (r *PostgresRepository) PushMessage(ctx context.Context, messageID uuid.UUID) (pushMessage, error) {
	m, err := r.q.PushMessage(ctx, messageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return pushMessage{}, apperr.ErrNotFound
	}
	if err != nil {
		return pushMessage{}, apperr.Wrap(err, "load message to push")
	}
	return pushMessage{
		CrewID: m.CrewID, ChannelID: m.ChannelID, Body: m.Body,
		CrewName: m.CrewName, ChannelName: m.ChannelName, AuthorName: m.AuthorName,
	}, nil
}

// PushRecipients is the members a new message should push.
func (r *PostgresRepository) PushRecipients(ctx context.Context, messageID uuid.UUID) ([]uuid.UUID, error) {
	to, err := r.q.PushRecipients(ctx, messageID)
	if err != nil {
		return nil, apperr.Wrap(err, "push recipients")
	}
	return to, nil
}

// AuthorBlockedFor says whether the message's author and viewerID have
// blocked each other, either way.
func (r *PostgresRepository) AuthorBlockedFor(ctx context.Context, viewerID, messageID uuid.UUID) (bool, error) {
	return r.q.AuthorBlockedFor(ctx, crewsdb.AuthorBlockedForParams{Viewer: viewerID, ID: messageID})
}

// update runs write and tells watchers the message changed, in one
// transaction.
func (r *PostgresRepository) update(ctx context.Context, meta messageMeta, write func(*crewsdb.Queries) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return apperr.Wrap(err, "begin message update")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = write(r.q.WithTx(tx)); err != nil {
		return apperr.Wrap(err, "update message")
	}
	if err = live.Notify(ctx, tx, live.Event{Type: live.EventUpdate, CrewID: meta.CrewID, ChannelID: meta.ChannelID, MessageID: meta.ID}); err != nil {
		return err
	}
	return apperr.Wrap(tx.Commit(ctx), "commit message update")
}

func (r *PostgresRepository) inTx(ctx context.Context, fn func(*crewsdb.Queries, pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return apperr.Wrap(err, "begin")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = fn(r.q.WithTx(tx), tx); err != nil {
		return err
	}
	return apperr.Wrap(tx.Commit(ctx), "commit")
}

// nameTaken turns a unique violation into errChannelNameTaken.
func nameTaken(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return errChannelNameTaken
	}
	return err
}

func toCrew(row crewsdb.Crew) Crew {
	return Crew{ID: row.ID, Name: row.Name, OwnerID: row.OwnerID, Code: row.Code}
}

func toChannel(r crewsdb.CrewChannel) Channel {
	return Channel{ID: r.ID, Kind: r.Kind, Name: r.Name, Topic: r.Topic, Position: int(r.Position), IsDefault: r.IsDefault}
}

// toMessage is a stored message as viewerID sees it, without reactions.
func toMessage(r crewsdb.MessagesAfterRow, viewerID uuid.UUID) Message {
	m := Message{
		ID: r.ID, Seq: r.Seq, ChannelID: r.ChannelID, Kind: r.Kind, Body: r.Body,
		Mentions: r.Mentions, MentionsMe: r.MentionAll || slices.Contains(r.Mentions, viewerID),
		Pinned: r.PinnedAt != nil, Edited: r.EditedAt != nil,
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

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
