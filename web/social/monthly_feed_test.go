package social

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/achievements/achievement"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

func TestFeedCardDrawsTheMonthlyCoinForAMetMonth(t *testing.T) {
	items := []achievement.Item{{
		ID: uuid.New(), DisplayName: "Bruno Lima", Kind: achievement.KindMonthlyChallengeMet,
		Title: "September 2026 challenge", SourceKey: "2026-09", MonthlyMetric: "workouts", MonthlyTarget: 12,
		OccurredAt: time.Now().Add(-time.Hour),
	}}
	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), "pt-PT")
	if err := feedCard(items).Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"data-feed-badges", "September 2026 challenge", ">SET<", ">12<", "#D4A017"} {
		if !strings.Contains(out, want) {
			t.Errorf("feed lacks %q: %s", want, out)
		}
	}
}
