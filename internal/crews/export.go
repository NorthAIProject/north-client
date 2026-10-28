package crews

import (
	"context"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/crews/crew"
)

// AuthoredBy is every crew message userID posted and kept, for their export.
func (s *Service) AuthoredBy(ctx context.Context, userID uuid.UUID) ([]crew.ExportedMessage, error) {
	return s.repo.MessagesAuthoredBy(ctx, userID)
}
