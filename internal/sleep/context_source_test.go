package sleep_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/sleep"
)

// The trend explains the week, last night explains today, so both are sent.
func TestContextSourceReportsTrendAndLastNight(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	today := sleep.LocalDate(user, timeNow())
	for i, minutes := range []int{465, 420, 400} {
		if _, err := svc.LogFor(ctx, user, today.AddDate(0, 0, -i), sleep.Input{
			DurationMinutes: minutes,
			Quality:         quality(4),
		}); err != nil {
			t.Fatalf("log night %d: %v", i, err)
		}
	}

	var into coach.Context
	if err := sleep.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, &into); err != nil {
		t.Fatalf("collect: %v", err)
	}

	if len(into.DailySignals) != 2 {
		t.Fatalf("DailySignals = %v, want trend plus last night", into.DailySignals)
	}

	joined := strings.Join(into.DailySignals, " | ")
	for _, want := range []string{"averaging", "3 nights", "Last night", "7h 45m"} {
		if !strings.Contains(joined, want) {
			t.Errorf("summaries %q missing %q", joined, want)
		}
	}
}

// Nothing logged is still worth saying, but there is no "last night" line to
// invent when the night is genuinely absent.
func TestContextSourceReportsAnEmptyLogWithoutInventingANight(t *testing.T) {
	svc, user := newService(t)

	var into coach.Context
	if err := sleep.NewContextSource(svc).Collect(context.Background(), coach.ContextRequest{User: user}, &into); err != nil {
		t.Fatalf("collect: %v", err)
	}

	if len(into.DailySignals) != 1 {
		t.Fatalf("DailySignals = %v, want only the trend line", into.DailySignals)
	}
	if !strings.Contains(into.DailySignals[0], "nothing logged yet") {
		t.Errorf("summary = %q, want an explicit empty note", into.DailySignals[0])
	}
}
