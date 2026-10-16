package outreach

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
)

const sweepPageSize = 100

// Sweeper visits every onboarded account once an hour and lets the coach
// speak first where something warrants it. The once-a-day claim makes a
// repeated hour, or a second worker, harmless.
type Sweeper struct {
	svc *Service
}

func NewSweeper(svc *Service) *Sweeper { return &Sweeper{svc: svc} }

func (sw *Sweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	if sw.svc == nil || sw.svc.users == nil {
		return nil
	}
	var after uuid.UUID
	sent := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		page, err := sw.svc.users.ListOnboarded(ctx, after, sweepPageSize)
		if err != nil {
			return err
		}
		for _, user := range page {
			ok, err := sw.svc.Evaluate(ctx, user)
			if err != nil {
				sw.svc.log.Error("outreach evaluate failed", slog.String("user_id", user.ID.String()), slog.Any("error", err))
			} else if ok {
				sent++
			}
			after = user.ID
		}
		if len(page) < sweepPageSize {
			break
		}
	}
	if sent > 0 {
		sw.svc.log.Info("coach outreach sent", slog.Int("sent", sent))
	}
	return nil
}
