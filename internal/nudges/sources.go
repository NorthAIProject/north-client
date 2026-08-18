package nudges

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/memories"
)

// WeekFrom is the first-week data the sweep needs, wired from existing slices.
type WeekFrom struct {
	Chats  userChats
	Photos photoStore
	Facts  factStore
}

type userChats interface {
	UserMessageCount(ctx context.Context, userID uuid.UUID) (int, error)
}

type photoStore interface {
	HasImage(ctx context.Context, userID uuid.UUID) (bool, error)
	LastImageAt(ctx context.Context, userID uuid.UUID) (time.Time, bool, error)
}

type factStore interface {
	ListApproved(ctx context.Context, userID uuid.UUID) ([]memories.Memory, error)
}

func (w WeekFrom) UserMessageCount(ctx context.Context, userID uuid.UUID) (int, error) {
	if w.Chats == nil {
		return 0, nil
	}
	return w.Chats.UserMessageCount(ctx, userID)
}

func (w WeekFrom) HasEvidence(ctx context.Context, userID uuid.UUID) (bool, error) {
	if w.Photos == nil {
		return false, nil
	}
	return w.Photos.HasImage(ctx, userID)
}

func (w WeekFrom) LastEvidenceAt(ctx context.Context, userID uuid.UUID) (time.Time, bool, error) {
	if w.Photos == nil {
		return time.Time{}, false, nil
	}
	return w.Photos.LastImageAt(ctx, userID)
}

func (w WeekFrom) HasLifeFocus(ctx context.Context, userID uuid.UUID, areas ...string) (bool, error) {
	if w.Facts == nil {
		return false, nil
	}
	list, err := w.Facts.ListApproved(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, m := range list {
		for _, area := range areas {
			if strings.Contains(strings.ToLower(m.Content), "focus area: "+strings.ToLower(area)) {
				return true, nil
			}
		}
	}
	return false, nil
}
