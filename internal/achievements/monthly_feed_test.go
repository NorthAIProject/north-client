package achievements_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/social"
)

func TestMonthlyFeedCardLocalised(t *testing.T) {
	pool := testdb.New(t)
	ach := achievements.NewService(pool)
	soc := social.NewService(social.NewRepository(pool))
	ana, leo := person(t, pool, "mf-ana@north.test", "Ana"), person(t, pool, "mf-leo@north.test", "Leo")
	follow(t, soc, leo, ana, "mfana")
	at := time.Now().UTC().Add(-time.Hour)
	if _, err := pool.Exec(context.Background(), `INSERT INTO monthly_challenges (user_id, month, metric, target) VALUES ($1, '2026-09-01', 'habit_days', 12)`, ana.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO achievements (user_id, category, kind, title, detail, occurred_at, source_key, private)
		VALUES ($1, 'awards', 'monthly_challenge_met', 'Challenge met', '', $2, '2026-09', false)`, ana.ID, at); err != nil {
		t.Fatal(err)
	}
	// A badge in the same instant must stay a separate card.
	if _, err := pool.Exec(context.Background(), `INSERT INTO achievements (user_id, category, kind, title, detail, occurred_at, source_key, private)
		VALUES ($1, 'awards', 'badge_earned', 'First workout', '', $2, 'workouts_1', false)`, ana.ID, at); err != nil {
		t.Fatal(err)
	}

	for _, viewer := range []string{"en", "es"} {
		ctx := i18n.WithLocale(context.Background(), viewer)
		got, err := ach.Feed(ctx, leo.ID, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("%s feed = %d items, want the medal and the badge apart", viewer, len(got))
		}
		var m achievements.Item
		for _, it := range got {
			if it.Kind == "monthly_challenge_met" {
				m = it
			}
		}
		if m.ID.String() == "00000000-0000-0000-0000-000000000000" || m.MonthlyMetric != "habit_days" || m.MonthlyTarget != 12 || m.Private {
			t.Fatalf("%s monthly item = %+v", viewer, m)
		}
		wantTitle := i18n.Tf(ctx, "monthly.medal.name", i18n.Date(ctx, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), "January 2006"))
		if m.Title != wantTitle || m.Detail != i18n.Tf(ctx, "monthly.medal.description.habit_days", 12, i18n.Date(ctx, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), "January")) {
			t.Fatalf("%s title/detail = %q / %q", viewer, m.Title, m.Detail)
		}
		if viewer == "es" && !strings.Contains(strings.ToLower(m.Title), "septiembre") {
			t.Fatalf("es title = %q, want Spanish", m.Title)
		}
		if viewer == "en" && m.Title != "September 2026 challenge" {
			t.Fatalf("en title = %q", m.Title)
		}
	}
}
