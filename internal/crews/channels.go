package crews

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"

	"github.com/NorthAIProject/north-client/internal/crews/crew"
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
func (s *Service) manager(ctx context.Context, crewID, actorID uuid.UUID) (Crew, error) {
	c, err := s.get(ctx, crewID, actorID)
	if err != nil {
		return Crew{}, err
	}
	role, err := s.role(ctx, c, actorID)
	if err != nil {
		return Crew{}, err
	}
	if !role.CanManage() {
		return Crew{}, apperr.ErrNotFound
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
	n, err := s.repo.CountChannels(ctx, crewID)
	if err != nil {
		return Channel{}, err
	}
	if n >= crew.MaxChannels {
		return Channel{}, apperr.FieldErrors{}.Add("name", i18n.T(ctx, "crews.err.channel-limit"))
	}
	ch, err := s.repo.CreateChannel(ctx, crewID, actorID, name, topic)
	if err != nil {
		return Channel{}, channelErr(ctx, err)
	}
	return ch, nil
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
	updated, err := s.repo.UpdateChannel(ctx, crewID, channelID, name, topic)
	if err != nil {
		return Channel{}, channelErr(ctx, err)
	}
	return updated, nil
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
	return s.repo.DeleteChannel(ctx, crewID, channelID)
}

// SetNotify is a member's own say over which messages in this crew push
// them, and until when they are muted.
func (s *Service) SetNotify(ctx context.Context, crewID, userID uuid.UUID, notify string, mutedUntil *time.Time) error {
	if notify != crew.NotifyAll && notify != crew.NotifyMentions && notify != crew.NotifyNone {
		return apperr.FieldErrors{}.Add("notify", i18n.T(ctx, "crews.err.notify"))
	}
	return s.repo.SetNotify(ctx, crewID, userID, notify, mutedUntil)
}

// textChannel is a text channel of crewID; #wins and other crews' channels
// are not found.
func (s *Service) textChannel(ctx context.Context, crewID, channelID uuid.UUID) (Channel, error) {
	ch, err := s.repo.ChannelInCrew(ctx, crewID, channelID)
	if err != nil {
		return Channel{}, err
	}
	if ch.Kind != crew.ChannelText {
		return Channel{}, apperr.ErrNotFound
	}
	return ch, nil
}

// channelErr turns a taken name into a field error.
func channelErr(ctx context.Context, err error) error {
	if errors.Is(err, errChannelNameTaken) {
		return apperr.FieldErrors{}.Add("name", i18n.T(ctx, "crews.err.channel-taken"))
	}
	return apperr.Wrap(err, "save channel")
}
