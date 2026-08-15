package health_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/health"
)

func TestSummaryReportsNothingWhenNothingWasMeasured(t *testing.T) {
	svc, user := newService(t)

	lines, err := svc.Summary(context.Background(), user.ID, at("2026-08-15T00:00:00Z"), 7)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("got %d lines, want 0 — an account with no device data invented some: %v", len(lines), lines)
	}
}

// A rate is averaged across the window. Reporting the sum of seven mornings'
// resting heart rate would be a number with no meaning.
func TestARateMetricIsAveragedAcrossTheWindow(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	for i, value := range []float64{50, 60, 70} {
		day := []string{"2026-08-12T06:00:00Z", "2026-08-13T06:00:00Z", "2026-08-14T06:00:00Z"}[i]
		if _, err := svc.Ingest(ctx, user.ID, "apple_health", []health.Reading{{
			Metric: "heart_rate", Value: value, Unit: "bpm", StartedAt: at(day),
		}}); err != nil {
			t.Fatalf("ingest: %v", err)
		}
	}

	lines, err := svc.Summary(ctx, user.ID, at("2026-08-15T00:00:00Z"), 7)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}

	line := findLine(t, lines, "heart rate")
	if !strings.Contains(line, "60") {
		t.Errorf("line = %q, want the mean of 50/60/70 (60), not a total", line)
	}
}

// A count is totalled per day. Averaging the raw rows would report a daily step
// count that depends on how often the phone happened to sync.
func TestACountMetricIsReportedPerDay(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	// Two rows on the same day: a phone that synced twice.
	for _, r := range []struct {
		when  string
		value float64
	}{
		{"2026-08-14T09:00:00Z", 4000},
		{"2026-08-14T18:00:00Z", 6000},
	} {
		if _, err := svc.Ingest(ctx, user.ID, "apple_health", []health.Reading{{
			Metric: "steps", Value: r.value, Unit: "count", StartedAt: at(r.when),
		}}); err != nil {
			t.Fatalf("ingest: %v", err)
		}
	}

	lines, err := svc.Summary(ctx, user.ID, at("2026-08-15T00:00:00Z"), 7)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}

	line := findLine(t, lines, "steps")
	if strings.Contains(line, "5,000") || strings.Contains(line, "5000") {
		t.Errorf("line = %q — the two syncs were averaged instead of totalled", line)
	}
	if !strings.Contains(line, "10,000") {
		t.Errorf("line = %q, want the day's total of 10,000", line)
	}
}

// Readings outside the window are not the coach's business: a resting heart
// rate from three months ago describes a different person's training state.
func TestReadingsOlderThanTheWindowAreExcluded(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	if _, err := svc.Ingest(ctx, user.ID, "apple_health", []health.Reading{{
		Metric: "heart_rate", Value: 99, Unit: "bpm", StartedAt: at("2026-05-01T06:00:00Z"),
	}}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	lines, err := svc.Summary(ctx, user.ID, at("2026-08-15T00:00:00Z"), 7)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("got %v, want nothing — a reading from May reached a 7-day window", lines)
	}
}

func findLine(t *testing.T, lines []string, want string) string {
	t.Helper()

	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), want) {
			return line
		}
	}
	t.Fatalf("no line mentioning %q in %v", want, lines)
	return ""
}
