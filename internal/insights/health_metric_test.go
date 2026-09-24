package insights

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/users"
)

type fakeHealth struct {
	asked  string
	stored []health.Stored
}

func (f *fakeHealth) Between(_ context.Context, _ uuid.UUID, metric string, _, _ time.Time) ([]health.Stored, error) {
	f.asked = metric
	return f.stored, nil
}

// The metric key is the page's; the reading name is what the phone sends.
// Mixing them up would chart nothing, with no error anywhere.
func TestHealthMetricsReadTheSyncedReadings(t *testing.T) {
	rg := weekRange(t)
	day := rg.Since.Add(26 * time.Hour)
	fake := &fakeHealth{stored: []health.Stored{{Metric: "resting_heart_rate", Value: 52, StartedAt: day}}}
	svc := &Service{health: fake}

	data, err := svc.Metric(context.Background(), users.User{}, rg, "resting-heart-rate")
	if err != nil {
		t.Fatalf("metric: %v", err)
	}
	if fake.asked != "resting_heart_rate" {
		t.Errorf("asked health for %q, want resting_heart_rate", fake.asked)
	}
	if len(data.Points) != 1 || data.Points[0].Value != 52 || !data.Points[0].At.Equal(day) {
		t.Errorf("points = %+v, want one 52 on %v", data.Points, day)
	}
	if data.Metric.Better != -1 {
		t.Error("a falling resting heart rate must read as improvement")
	}
}

// A deployment with no health data still serves the page, empty.
func TestHealthMetricsWithoutHealthAreEmpty(t *testing.T) {
	data, err := (&Service{}).Metric(context.Background(), users.User{}, weekRange(t), "steps")
	if err != nil || len(data.Points) != 0 {
		t.Errorf("metric without health = %+v, %v; want empty, nil", data.Points, err)
	}
}
