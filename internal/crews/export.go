package crews

import (
	"context"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/crews/crew"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// AuthoredBy is every crew message userID posted and kept, for their export.
func (s *Service) AuthoredBy(ctx context.Context, userID uuid.UUID) ([]crew.ExportedMessage, error) {
	rows, err := s.q.MessagesAuthoredBy(ctx, &userID)
	if err != nil {
		return nil, apperr.Wrap(err, "messages authored by")
	}
	out := make([]crew.ExportedMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, crew.ExportedMessage{Crew: r.CrewName, Channel: r.ChannelName, Body: r.Body, CreatedAt: r.CreatedAt, Edited: r.Edited})
	}
	return out, nil
}
