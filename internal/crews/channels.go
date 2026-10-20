package crews

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/text/unicode/norm"

	"github.com/NorthAIProject/north-client/internal/crews/crew"
	crewsdb "github.com/NorthAIProject/north-client/internal/crews/db"
	"github.com/NorthAIProject/north-client/internal/crews/live"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// channelName turns what someone typed into a channel name: lower case,
// accents dropped, spaces and underscores as dashes, anything else gone.
// "Leg Day!" is leg-day.
func channelName(raw string) string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(strings.ToLower(strings.TrimSpace(raw))) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '-':
			if s := b.String(); s != "" && !strings.HasSuffix(s, "-") {
				b.WriteRune('-')
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func validChannel(ctx context.Context, rawName, topic string) (string, string, error) {
	name := channelName(rawName)
	if name == "" || len(name) > 30 {
		return "", "", apperr.FieldErrors{}.Add("name", i18n.T(ctx, "crews.err.channel-name"))
	}
	topic = strings.TrimSpace(topic)
	if utf8.RuneCountInString(topic) > 120 {
		return "", "", apperr.FieldErrors{}.Add("topic", i18n.T(ctx, "crews.err.topic"))
	}
	return name, topic, nil
}

// manager is the crew when actorID runs it; not found otherwise, like every
// other owner/admin-only refusal.
func (s *Service) manager(ctx context.Context, crewID, actorID uuid.UUID) (crewsdb.Crew, error) {
	c, err := s.get(ctx, crewID, actorID)
	if err != nil {
		return crewsdb.Crew{}, err
	}
	role, err := s.role(ctx, c, actorID)
	if err != nil {
		return crewsdb.Crew{}, err
	}
	if !role.CanManage() {
		return crewsdb.Crew{}, apperr.ErrNotFound
	}
	return c, nil
}

// CreateChannel adds a text channel; whoever runs the crew may.
func (s *Service) CreateChannel(ctx context.Context, crewID, actorID uuid.UUID, rawName, topic string) (Channel, error) {
	if _, err := s.manager(ctx, crewID, actorID); err != nil {
		return Channel{}, err
	}
	name, topic, err := validChannel(ctx, rawName, topic)
	if err != nil {
		return Channel{}, err
	}
	n, err := s.q.CountChannels(ctx, crewID)
	if err != nil {
		return Channel{}, apperr.Wrap(err, "count channels")
	}
	if n >= crew.MaxChannels {
		return Channel{}, apperr.FieldErrors{}.Add("name", i18n.T(ctx, "crews.err.channel-limit"))
	}
	var row crewsdb.CrewChannel
	err = s.inTx(ctx, func(q *crewsdb.Queries, tx pgx.Tx) error {
		var insErr error
		if row, insErr = q.InsertChannel(ctx, crewsdb.InsertChannelParams{CrewID: crewID, Name: name, Topic: topic, CreatedBy: &actorID}); insErr != nil {
			return insErr
		}
		return live.Notify(ctx, tx, live.Event{Type: live.EventChannels, CrewID: crewID})
	})
	if err != nil {
		return Channel{}, channelErr(ctx, err)
	}
	return toChannel(row), nil
}

// UpdateChannel renames a text channel or changes its topic. #general keeps
// its name.
func (s *Service) UpdateChannel(ctx context.Context, crewID, channelID, actorID uuid.UUID, rawName, topic string) (Channel, error) {
	if _, err := s.manager(ctx, crewID, actorID); err != nil {
		return Channel{}, err
	}
	ch, err := s.textChannel(ctx, crewID, channelID)
	if err != nil {
		return Channel{}, err
	}
	name, topic, err := validChannel(ctx, rawName, topic)
	if err != nil {
		return Channel{}, err
	}
	if ch.IsDefault && name != ch.Name {
		return Channel{}, apperr.FieldErrors{}.Add("name", i18n.T(ctx, "crews.err.channel-keep"))
	}
	var row crewsdb.CrewChannel
	err = s.inTx(ctx, func(q *crewsdb.Queries, tx pgx.Tx) error {
		var upErr error
		if row, upErr = q.UpdateChannel(ctx, crewsdb.UpdateChannelParams{ID: channelID, Name: name, Topic: topic}); upErr != nil {
			return upErr
		}
		return live.Notify(ctx, tx, live.Event{Type: live.EventChannels, CrewID: crewID})
	})
	if err != nil {
		return Channel{}, channelErr(ctx, err)
	}
	return toChannel(row), nil
}

// DeleteChannel removes a text channel and its messages. #general stays.
func (s *Service) DeleteChannel(ctx context.Context, crewID, channelID, actorID uuid.UUID) error {
	if _, err := s.manager(ctx, crewID, actorID); err != nil {
		return err
	}
	ch, err := s.textChannel(ctx, crewID, channelID)
	if err != nil {
		return err
	}
	if ch.IsDefault {
		return apperr.FieldErrors{}.Add("channel", i18n.T(ctx, "crews.err.channel-keep"))
	}
	return s.inTx(ctx, func(q *crewsdb.Queries, tx pgx.Tx) error {
		if delErr := q.DeleteChannel(ctx, channelID); delErr != nil {
			return apperr.Wrap(delErr, "delete channel")
		}
		return live.Notify(ctx, tx, live.Event{Type: live.EventChannels, CrewID: crewID})
	})
}

// SetNotify is a member's own say over which messages in this crew push
// them, and until when they are muted.
func (s *Service) SetNotify(ctx context.Context, crewID, userID uuid.UUID, notify string, mutedUntil *time.Time) error {
	if notify != crew.NotifyAll && notify != crew.NotifyMentions && notify != crew.NotifyNone {
		return apperr.FieldErrors{}.Add("notify", i18n.T(ctx, "crews.err.notify"))
	}
	n, err := s.q.SetNotify(ctx, crewsdb.SetNotifyParams{CrewID: crewID, UserID: userID, Notify: notify, MutedUntil: mutedUntil})
	if err != nil {
		return apperr.Wrap(err, "set notify")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// textChannel is a text channel of crewID; #wins and other crews' channels
// are not found.
func (s *Service) textChannel(ctx context.Context, crewID, channelID uuid.UUID) (crewsdb.CrewChannel, error) {
	ch, err := s.q.ChannelInCrew(ctx, crewsdb.ChannelInCrewParams{ID: channelID, CrewID: crewID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && ch.Kind != crew.ChannelText) {
		return crewsdb.CrewChannel{}, apperr.ErrNotFound
	}
	if err != nil {
		return crewsdb.CrewChannel{}, apperr.Wrap(err, "channel")
	}
	return ch, nil
}

func (s *Service) inTx(ctx context.Context, fn func(*crewsdb.Queries, pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return apperr.Wrap(err, "begin")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = fn(s.q.WithTx(tx), tx); err != nil {
		return err
	}
	return apperr.Wrap(tx.Commit(ctx), "commit")
}

// channelErr turns a taken name into a field error.
func channelErr(ctx context.Context, err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return apperr.FieldErrors{}.Add("name", i18n.T(ctx, "crews.err.channel-taken"))
	}
	return apperr.Wrap(err, "save channel")
}

func toChannel(r crewsdb.CrewChannel) Channel {
	return Channel{ID: r.ID, Kind: r.Kind, Name: r.Name, Topic: r.Topic, Position: int(r.Position), IsDefault: r.IsDefault}
}
