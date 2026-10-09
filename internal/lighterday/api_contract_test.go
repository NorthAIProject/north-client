package lighterday

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/insights"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	"github.com/NorthAIProject/north-client/internal/users"
)

// lowMorning is two weeks of HRV around 52 ms and resting heart rate around
// 56 bpm, then a morning of 38 ms and 61 bpm.
type lowMorning struct{ at time.Time }

func (l lowMorning) Between(_ context.Context, _ uuid.UUID, metric string, _, _ time.Time) ([]health.Stored, error) {
	base, today := 52.0, 38.0
	if metric == "resting_heart_rate" {
		base, today = 56, 61
	}
	var out []health.Stored
	for d := 1; d <= 14; d++ {
		out = append(out, health.Stored{Value: base + float64(d%3-1), StartedAt: l.at.AddDate(0, 0, -d)})
	}
	return append(out, health.Stored{Value: today, StartedAt: l.at}), nil
}

func TestLighterShape(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 7, 6, 30, 0, 0, time.UTC)
	r, err := insights.NewRecoverySource(lowMorning{at: at}, nil).Recovery(context.Background(), users.User{Timezone: "UTC"}, at)
	if err != nil || !r.Low() {
		t.Fatalf("sample recovery = %+v, %v; want low", r.Score, err)
	}
	apitest.AssertGolden(t, "today-lighter.golden.json", project(Today{
		Recovery: r,
		Session:  "Lower body",
		Due:      true,
	}))
}
