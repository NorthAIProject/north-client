package hydration_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/hydration"
)

func TestContextSourceReportsTodaysIntake(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	for _, amount := range []int{500, 500, 250} {
		if _, err := svc.Log(ctx, user, amount); err != nil {
			t.Fatalf("log: %v", err)
		}
	}

	var into coach.Context
	if err := hydration.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, &into); err != nil {
		t.Fatalf("collect: %v", err)
	}

	if len(into.DailySignals) != 1 {
		t.Fatalf("DailySignals = %v, want one entry", into.DailySignals)
	}
	for _, want := range []string{"1.2L", "2.0L target", "3 drinks"} {
		if !strings.Contains(into.DailySignals[0], want) {
			t.Errorf("summary %q missing %q", into.DailySignals[0], want)
		}
	}
}

// An empty day is reported rather than skipped: "drank nothing" is something
// the coach should be able to act on, and silence would let it assume the day
// went fine.
func TestContextSourceReportsAnEmptyDayRatherThanStayingSilent(t *testing.T) {
	svc, user := newService(t)

	var into coach.Context
	if err := hydration.NewContextSource(svc).Collect(context.Background(), coach.ContextRequest{User: user}, &into); err != nil {
		t.Fatalf("collect: %v", err)
	}

	if len(into.DailySignals) != 1 || !strings.Contains(into.DailySignals[0], "nothing logged") {
		t.Errorf("DailySignals = %v, want an explicit empty-day note", into.DailySignals)
	}
}
