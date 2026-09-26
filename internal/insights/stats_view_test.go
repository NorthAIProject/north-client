package insights

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"

	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/stats"
	"github.com/NorthAIProject/north-client/internal/stats/stat"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

func renderString(t *testing.T, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestStatsPanelsRender(t *testing.T) {
	t.Parallel()
	rg := timerange.Parse(timerange.KeyMonth, time.UTC)
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	at := func(d, h int) *time.Time { t := day(d).Add(time.Duration(h) * time.Hour); return &t }

	sleepHTML := renderString(t, insightpages.SleepPanels(buildSleepView(rg, stat.Sleep([]stat.Night{
		{Date: day(2), Minutes: 450, Start: at(1, 23), End: at(2, 7), Stages: map[string]int{"deep": 80, "rem": 90, "core": 280}},
		{Date: day(3), Minutes: 400},
	}, 480))))
	for _, want := range []string{`data-testid="insights-sleep"`, "7h 05m", "Deep", "23:00"} {
		if !strings.Contains(sleepHTML, want) {
			t.Errorf("sleep panels lack %q", want)
		}
	}

	cardioHTML := renderString(t, insightpages.CardioPanels(buildCardioView(rg, stats.CardioStats{
		CardioStats: stat.Cardio([]stat.Session{{Code: "running", Name: "Running", At: day(2), Seconds: 1500, DistanceM: 5000}}, rg.Since, rg.Until),
	})))
	for _, want := range []string{`data-testid="insights-cardio"`, "5:00 /km", "Best 5K", "25:00"} {
		if !strings.Contains(cardioHTML, want) {
			t.Errorf("cardio panels lack %q", want)
		}
	}

	patternsHTML := renderString(t, insightpages.PatternsPanels(buildPatternsView(rg, 90, []stat.Finding{{
		Title: "You sleep 42 min less after late caffeine", Unit: "min",
		A: stat.Group{Label: "After late caffeine", Mean: 398, N: 8}, B: stat.Group{Label: "Other nights", Mean: 440, N: 20},
	}})))
	for _, want := range []string{"42 min less", "6h 38m", "(8 days)"} {
		if !strings.Contains(patternsHTML, want) {
			t.Errorf("patterns panels lack %q", want)
		}
	}

	eatingHTML := renderString(t, insightpages.NutritionBody(insightpages.NutritionView{
		HasData: true,
		Eating:  buildEatingView(stat.Eating([]stat.FoodEntry{{At: *at(2, 22), Date: day(2), Label: "Pizza", Kcal: 900}}, 2000, 150, 80, 21)),
	}))
	for _, want := range []string{`data-testid="insights-eating"`, "Pizza", "Late (after 21)"} {
		if !strings.Contains(eatingHTML, want) {
			t.Errorf("eating section lacks %q", want)
		}
	}
}
