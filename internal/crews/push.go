package crews

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/jobs"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// ThreadSender delivers one notification, grouped with others of the same
// thread. The APNs and web-push services both implement it.
type ThreadSender interface {
	SendThread(ctx context.Context, userID uuid.UUID, title, body, href, category, threadID string) (int, error)
}

// pushExcerpt bounds the message text in a notification; the lock screen
// shows a few lines at most.
const pushExcerpt = 180

// WithPushers sets where new-message pushes go. Without any, the push job
// does nothing.
func (c *Chat) WithPushers(pushers ...ThreadSender) *Chat {
	for _, p := range pushers {
		if p != nil {
			c.pushers = append(c.pushers, p)
		}
	}
	return c
}

// HandlePushJob tells the members who asked about a new message. A message
// deleted or hidden before the job ran is not pushed. Quiet hours are not
// consulted: the phone's own Focus settings govern chat.
//
// A failed delivery is logged, not returned: a retry would push again to
// everyone the first run already reached.
func (c *Chat) HandlePushJob(ctx context.Context, payload json.RawMessage) error {
	var p jobs.CrewMessagePushPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return apperr.Wrap(err, "decode crew message push job")
	}
	m, err := c.repo.PushMessage(ctx, p.MessageID)
	if errors.Is(err, apperr.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	to, err := c.repo.PushRecipients(ctx, p.MessageID)
	if err != nil {
		return err
	}
	title := m.CrewName + " · #" + m.ChannelName
	body := m.AuthorName + ": " + excerpt(m.Body, pushExcerpt)
	href := "/app/crews/" + m.CrewID.String() + "/c/" + m.ChannelID.String()
	thread := "crew-" + m.CrewID.String()
	for _, userID := range to {
		for _, s := range c.pushers {
			if _, err := s.SendThread(ctx, userID, title, body, href, "CREW_MESSAGE", thread); err != nil {
				c.log.Warn("crew message push failed", slog.String("user_id", userID.String()), slog.Any("error", err))
			}
		}
	}
	return nil
}
