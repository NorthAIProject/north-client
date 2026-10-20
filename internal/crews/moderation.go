package crews

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	crewsdb "github.com/NorthAIProject/north-client/internal/crews/db"
	"github.com/NorthAIProject/north-client/internal/crews/live"
	"github.com/NorthAIProject/north-client/internal/moderation"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// MessageSubject is the moderation kind for a crew message.
const MessageSubject = "crew_message"

// Report flags a message someone can see. It disappears for them at once;
// enough reporters hide it for the whole crew.
func (c *Chat) Report(ctx context.Context, crewID, messageID, reporterID uuid.UUID, reason, note string) error {
	meta, _, err := c.visibleMeta(ctx, crewID, messageID, reporterID)
	if err != nil {
		return err
	}
	if meta.AuthorID == nil {
		return apperr.ErrNotFound // a system message says nothing anyone wrote
	}
	_, err = c.mod.Report(ctx, reporterID, moderation.Subject{
		Kind: MessageSubject, ID: messageID, AuthorID: *meta.AuthorID, ScopeID: &crewID,
	}, reason, note)
	return err
}

// Hider is the crews side of moderation, registered under MessageSubject.
func (c *Chat) Hider() moderation.Hider { return messageHider{c} }

type messageHider struct{ c *Chat }

func (h messageHider) Hide(ctx context.Context, id uuid.UUID) error { return h.set(ctx, id, true) }

func (h messageHider) Unhide(ctx context.Context, id uuid.UUID) error { return h.set(ctx, id, false) }

func (h messageHider) set(ctx context.Context, id uuid.UUID, hide bool) error {
	tx, err := h.c.pool.Begin(ctx)
	if err != nil {
		return apperr.Wrap(err, "begin hide")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := h.c.q.WithTx(tx).SetHidden(ctx, crewsdb.SetHiddenParams{ID: id, Hide: hide})
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

// Threshold is how many distinct reporters hide a message: two in a crew of
// four or fewer, where three would be most of the room, else three.
func (h messageHider) Threshold(ctx context.Context, id uuid.UUID) (int, error) {
	n, err := h.c.q.MessageCrewSize(ctx, id)
	if err != nil {
		return 0, apperr.Wrap(err, "crew size")
	}
	if n <= 4 {
		return 2, nil
	}
	return 3, nil
}

func (h messageHider) Excerpt(ctx context.Context, id uuid.UUID) (string, error) {
	body, err := h.c.q.MessageBody(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", apperr.Wrap(err, "message body")
	}
	return excerpt(body, 200), nil
}
