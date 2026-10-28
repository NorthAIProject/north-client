package crews

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/crews/crew"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// Unread is how much of one channel a member has not read, and how much of
// that is for them.
type Unread = crew.Unread

// Edit changes the words of your own message.
func (c *Chat) Edit(ctx context.Context, crewID, messageID, actorID uuid.UUID, body string) (Message, error) {
	meta, _, err := c.visibleMeta(ctx, crewID, messageID, actorID)
	if err != nil {
		return Message{}, err
	}
	if meta.AuthorID == nil || *meta.AuthorID != actorID || meta.DeletedAt != nil {
		return Message{}, apperr.ErrNotFound
	}
	// Rewriting a message is posting it again: a suspension stops both.
	if err = c.mod.CanPost(ctx, actorID); err != nil {
		return Message{}, err
	}
	if body, err = c.validBody(ctx, body); err != nil {
		return Message{}, err
	}
	handles, _ := parseMentions(body)
	mentions := []uuid.UUID{}
	if len(handles) > 0 {
		if mentions, err = c.repo.MemberIDsByHandle(ctx, crewID, handles); err != nil {
			return Message{}, err
		}
	}
	if err = c.repo.EditMessage(ctx, meta, body, mentions); err != nil {
		return Message{}, err
	}
	return c.Get(ctx, crewID, messageID, actorID)
}

// Delete removes a message's words, keeping its place: its author may, and
// so may whoever runs the crew.
func (c *Chat) Delete(ctx context.Context, crewID, messageID, actorID uuid.UUID) error {
	meta, role, err := c.visibleMeta(ctx, crewID, messageID, actorID)
	if err != nil {
		return err
	}
	own := meta.AuthorID != nil && *meta.AuthorID == actorID
	if !own && !role.CanManage() {
		return apperr.ErrNotFound
	}
	return c.repo.DeleteMessage(ctx, meta, actorID)
}

// React adds (on) or takes back one of the crew emoji. Doing either twice
// is a no-op.
func (c *Chat) React(ctx context.Context, crewID, messageID, userID uuid.UUID, emoji string, on bool) error {
	if crew.Glyph(emoji) == "" {
		return apperr.FieldErrors{}.Add("emoji", i18n.T(ctx, "crews.err.emoji"))
	}
	meta, _, err := c.visibleMeta(ctx, crewID, messageID, userID)
	if err != nil {
		return err
	}
	if meta.DeletedAt != nil {
		return apperr.ErrNotFound
	}
	return c.repo.SetReaction(ctx, meta, userID, emoji, on)
}

// Pin pins (on) or unpins a message; whoever runs the crew may.
func (c *Chat) Pin(ctx context.Context, crewID, messageID, actorID uuid.UUID, on bool) error {
	meta, role, err := c.visibleMeta(ctx, crewID, messageID, actorID)
	if err != nil {
		return err
	}
	if !role.CanManage() || meta.DeletedAt != nil {
		return apperr.ErrNotFound
	}
	if on && meta.PinnedAt == nil {
		n, cErr := c.repo.CountPins(ctx, meta.ChannelID)
		if cErr != nil {
			return cErr
		}
		if n >= crew.MaxPins {
			return apperr.FieldErrors{}.Add("pin", i18n.T(ctx, "crews.err.pins"))
		}
	}
	return c.repo.SetPin(ctx, meta, actorID, on)
}

// Pins are a channel's pinned messages as viewerID sees them, newest pin
// first.
func (c *Chat) Pins(ctx context.Context, crewID, channelID, viewerID uuid.UUID) ([]Message, error) {
	if _, _, err := c.member(ctx, crewID, viewerID); err != nil {
		return nil, err
	}
	if err := c.textChannel(ctx, crewID, channelID); err != nil {
		return nil, err
	}
	ids, err := c.repo.PinnedIDs(ctx, channelID)
	if err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(ids))
	for _, id := range ids {
		m, getErr := c.Get(ctx, crewID, id, viewerID)
		if apperr.Is(getErr, apperr.ErrNotFound) {
			continue // hidden from this viewer
		}
		if getErr != nil {
			return nil, getErr
		}
		out = append(out, m)
	}
	return out, nil
}

// MarkRead records that userID has read channelID up to seq. It never moves
// backwards, so a late request from an old tab does not resurrect unread.
func (c *Chat) MarkRead(ctx context.Context, crewID, channelID, userID uuid.UUID, seq int64) error {
	if _, _, err := c.member(ctx, crewID, userID); err != nil {
		return err
	}
	if err := c.textChannel(ctx, crewID, channelID); err != nil {
		return err
	}
	return c.repo.MarkRead(ctx, channelID, userID, seq)
}

// Unread is, for every text channel with something new for userID, how
// much and how much of it is for them.
func (c *Chat) Unread(ctx context.Context, userID uuid.UUID) ([]Unread, error) {
	return c.repo.Unread(ctx, userID)
}

// visibleMeta is a message's facts and the actor's role, when the actor is a
// member who can see the message.
func (c *Chat) visibleMeta(ctx context.Context, crewID, messageID, actorID uuid.UUID) (messageMeta, Role, error) {
	_, role, err := c.member(ctx, crewID, actorID)
	if err != nil {
		return messageMeta{}, "", err
	}
	if _, err = c.repo.VisibleMessage(ctx, actorID, messageID, crewID); err != nil {
		if errors.Is(err, apperr.ErrNotFound) {
			return messageMeta{}, "", err
		}
		return messageMeta{}, "", apperr.Wrap(err, "message for viewer")
	}
	meta, err := c.repo.MessageMeta(ctx, crewID, messageID)
	if err != nil {
		return messageMeta{}, "", err
	}
	return meta, role, nil
}
