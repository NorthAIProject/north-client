package horizon

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/insights"
	"github.com/NorthAIProject/north-client/internal/users"
)

// RecoveryReader is the one recovery rule in Khepri, read for any morning.
type RecoveryReader interface {
	Recovery(ctx context.Context, user users.User, now time.Time) (insights.RecoveryData, error)
}

// recoveryMornings is how many mornings back the lighter-week rule looks.
const recoveryMornings = 7

// MorningRecovery counts low mornings with the same rule the lighter-day
// offer uses, so "low" means one thing everywhere.
type MorningRecovery struct {
	reader RecoveryReader
}

func NewMorningRecovery(reader RecoveryReader) *MorningRecovery {
	return &MorningRecovery{reader: reader}
}

// LowMornings is how many of the last seven mornings, today included, read
// low. A morning with no readings is not low.
func (m *MorningRecovery) LowMornings(ctx context.Context, user users.User, now time.Time) (int, error) {
	low := 0
	for back := 0; back < recoveryMornings; back++ {
		r, err := m.reader.Recovery(ctx, user, now.AddDate(0, 0, -back))
		if err != nil {
			return 0, err
		}
		if r.Low() {
			low++
		}
	}
	return low, nil
}
