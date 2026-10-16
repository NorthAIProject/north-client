// Package recaps renders the last-7/30-days recap page and the "your week"
// strip on My Day.
package recaps

import (
	"context"
	"fmt"
	"strings"

	"github.com/NorthAIProject/north-client/internal/recaps/recap"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	"github.com/NorthAIProject/north-client/web/shared/ui/chart"
)

// Notice is what the page says after a post to the friends feed.
type Notice string

const (
	NoticeNone Notice = ""
	// NoticePosted: friends can see it.
	NoticePosted Notice = "posted"
	// NoticeHidden: posted, but training is not shared, so nobody sees it.
	NoticeHidden Notice = "hidden"
)

// View is the recap page.
type View struct {
	Key     string
	Options []Option
	// Period is "9 Oct – 15 Oct".
	Period      string
	Tiles       []Tile
	Chart       chart.Props
	HasActivity bool
	Highlights  []recap.Highlight
	// ShareText and ShareURL are what the share button hands the browser.
	ShareText string
	ShareURL  string
	Notice    Notice
}

// Option is one choice in the 7/30 days toggle.
type Option struct {
	Key      string
	Label    string
	Selected bool
}

// Tile is one number. Delta is against the window before; HasPrior false
// shows none.
type Tile struct {
	Label    string
	Value    int
	Delta    int
	HasPrior bool
}

// BuildView turns a recap into the page. siteURL is what a shared recap links
// back to.
func BuildView(ctx context.Context, r recap.Recap, siteURL string, notice Notice) View {
	v := View{
		Key:         r.Key,
		Period:      i18n.Date(ctx, r.Since, "2 Jan") + " – " + i18n.Date(ctx, r.Until, "2 Jan"),
		HasActivity: r.Totals.ActiveDays > 0,
		Highlights:  r.Highlights,
		ShareURL:    siteURL,
		Notice:      notice,
	}
	for _, key := range []string{recap.KeyWeek, recap.KeyMonth} {
		v.Options = append(v.Options, Option{Key: key, Label: i18n.T(ctx, "range."+key), Selected: key == r.Key})
	}
	tile := func(labelKey string, now, before int) Tile {
		return Tile{Label: i18n.T(ctx, labelKey), Value: now, Delta: now - before, HasPrior: true}
	}
	v.Tiles = []Tile{
		tile("recap.tile.active", r.Totals.ActiveDays, r.Prior.ActiveDays),
		tile("recap.tile.workouts", r.Totals.Workouts, r.Prior.Workouts),
		tile("recap.tile.sets", r.Totals.Sets, r.Prior.Sets),
		tile("recap.tile.food", r.Totals.FoodLogs, r.Prior.FoodLogs),
		tile("recap.tile.checkins", r.Totals.CheckIns, r.Prior.CheckIns),
		{Label: i18n.T(ctx, "recap.tile.streak"), Value: r.CheckInStreak},
	}
	v.Chart = dailyChart(ctx, "recap-daily", r)
	v.ShareText = ShareText(ctx, r)
	return v
}

// dailyChart stacks what each day had — training, food, a check-in — one unit
// each, so a day of forty sets does not dwarf a day of three meals: the
// question is which days showed up, not how much each logged.
func dailyChart(ctx context.Context, id string, r recap.Recap) chart.Props {
	layout := "Mon"
	if r.Key == recap.KeyMonth {
		layout = "2"
	}
	labels := make([]string, 0, len(r.Days))
	var training, food, checkIns []float64
	for _, d := range r.Days {
		labels = append(labels, i18n.Date(ctx, d.Date, layout))
		training = append(training, one(d.Workouts+d.Sets))
		food = append(food, one(d.FoodLogs))
		checkIns = append(checkIns, one(d.CheckIns))
	}
	return viz.StackedBar(id, labels, []viz.BarSeries{
		{Label: i18n.T(ctx, "recap.chart.training"), Values: training},
		{Label: i18n.T(ctx, "recap.chart.food"), Values: food},
		{Label: i18n.T(ctx, "recap.chart.checkins"), Values: checkIns},
	})
}

// Strip is the compact "your week" line on My Day.
type Strip struct {
	ActiveDays int
	Workouts   int
	CheckIns   int
	Chart      chart.Props
}

// BuildStrip is the strip for a week's recap.
func BuildStrip(r recap.Recap) Strip {
	labels := make([]string, 0, len(r.Days))
	values := make([]float64, 0, len(r.Days))
	for _, d := range r.Days {
		labels = append(labels, d.Date.Format("Mon"))
		values = append(values, one(d.Workouts+d.Sets)+one(d.FoodLogs)+one(d.CheckIns))
	}
	sparkline := viz.Sparkline("recap-strip", labels, values)
	sparkline.Class = "h-8 w-24"
	return Strip{ActiveDays: r.Totals.ActiveDays, Workouts: r.Totals.Workouts, CheckIns: r.Totals.CheckIns, Chart: sparkline}
}

// ShareText is the recap in one line for a message: the window and the
// non-zero numbers, written "label: value" so no language needs plurals.
func ShareText(ctx context.Context, r recap.Recap) string {
	parts := []string{fmt.Sprintf("%s: %d", i18n.T(ctx, "recap.tile.active"), r.Totals.ActiveDays)}
	for _, p := range []struct {
		key string
		n   int
	}{
		{"recap.tile.workouts", r.Totals.Workouts},
		{"recap.tile.sets", r.Totals.Sets},
		{"recap.tile.food", r.Totals.FoodLogs},
		{"recap.tile.checkins", r.Totals.CheckIns},
	} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%s: %d", i18n.T(ctx, p.key), p.n))
		}
	}
	return i18n.Tf(ctx, "recap.share.text."+r.Key, strings.Join(parts, " · "))
}

func one(n int) float64 {
	if n > 0 {
		return 1
	}
	return 0
}
