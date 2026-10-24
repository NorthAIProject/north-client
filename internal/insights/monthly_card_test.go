package insights

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	awardpages "github.com/NorthAIProject/north-client/web/awards"
)

func TestSummaryCardShowsTheMonthlyProgressLine(t *testing.T) {
	ctx := context.Background()
	open := award.MonthlyChallenge(ctx, "2026-10", "workouts", 8, 3, false)
	var buf bytes.Buffer
	if err := awardpages.SummaryCard(2, 31, nil, &open).Render(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "3 of 8 workouts") {
		t.Errorf("no progress text: %s", buf.String())
	}
	met := award.MonthlyChallenge(ctx, "2026-10", "workouts", 8, 8, true)
	buf.Reset()
	if err := awardpages.SummaryCard(2, 31, nil, &met).Render(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Challenge met") {
		t.Errorf("no met text: %s", buf.String())
	}
	buf.Reset()
	if err := awardpages.SummaryCard(2, 31, nil, nil).Render(ctx, &buf); err != nil || strings.Contains(buf.String(), "data-monthly-summary") {
		t.Error("a nil challenge still draws a line")
	}
}
