package vault

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// HandleSyncJob adapts the queue payload.
func (s *Service) HandleSyncJob(ctx context.Context, payload json.RawMessage) error {
	var p struct {
		UserID uuid.UUID `json:"user_id"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return apperr.Wrap(err, "decode sync vault job")
	}
	return s.Sync(ctx, p.UserID)
}
