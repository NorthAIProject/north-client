package fitness

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
)

// trendStateKind is which of the page's five shapes to render.
//
// An explicit state rather than a chain of if/else in the template. The page
// once had four conditions and only three branches: Unavailable was set by the
// handler, documented in a comment as making the template "say the activities
// could not be read", and never tested for — so a failed read rendered as
// "nothing imported yet", telling someone their history was empty when it was
// merely unreadable.
type trendStateKind int

const (
	stateChart trendStateKind = iota
	stateUnconfigured
	stateDisconnected
	stateUnavailable
	stateEmpty
)

func trendState(status strava.Status, trend strava.Trend) trendStateKind {
	switch {
	case !status.Configured:
		return stateUnconfigured
	case !status.Connected:
		return stateDisconnected
	case status.Unavailable:
		return stateUnavailable
	case trend.Sessions == 0:
		return stateEmpty
	default:
		return stateChart
	}
}

// ---------------------------------------------------------------------------
// The sentence.
//
// strava.TrendShape is the judgement — which band the last seven days fall in
// against what is normal. This is the wording, and it lives here because the
// domain layer has no business holding a vocabulary. The split is the one
// trendState already uses: decide in Go, phrase at the edge.

// trendSentence is what the chart means, in one line.
func trendSentence(trend strava.Trend) string {
	switch trend.Shape {
	case strava.ShapeNoHistory:
		return "Nothing recorded in this window yet."
	case strava.ShapeTooNew:
		// Deliberately not a verdict. There is training here, but not yet
		// enough of it to say whether this week is a lot or a little, and
		// inventing a trend out of a fortnight would be a confident lie.
		return fmt.Sprintf(
			"%s so far this week. A few more weeks and this will start comparing them.",
			humanMinutes(trend.RecentMinutes),
		)
	default:
		return fmt.Sprintf("%s this week — %s.", humanMinutes(trend.RecentMinutes), comparison(trend))
	}
}

// comparison is the clause that puts this week next to a normal one.
func comparison(trend strava.Trend) string {
	switch trend.Shape {
	case strava.ShapeBigWeek:
		return fmt.Sprintf("%s more than you normally do", proportion(trend.Ratio))
	case strava.ShapeBuilding:
		return fmt.Sprintf("%s above your normal", proportion(trend.Ratio))
	case strava.ShapeSteady:
		return "about what you normally do"
	case strava.ShapeEasier:
		return fmt.Sprintf("%s below your normal, an easier week", proportion(trend.Ratio))
	default:
		return "well below your normal"
	}
}

// proportion says how far off normal in the words people use, rather than as a
// percentage nobody reads aloud. "A third above" lands; "up 34.2%" does not.
func proportion(ratio float64) string {
	off := ratio - 1
	if off < 0 {
		off = -off
	}

	switch {
	case off >= 0.9:
		return "nearly double"
	case off >= 0.6:
		return "half again"
	case off >= 0.4:
		return "a half"
	case off >= 0.28:
		return "a third"
	case off >= 0.2:
		return "a quarter"
	default:
		return "a little"
	}
}

// streakSentence is the second line: how long this has been kept up.
//
// Only shown once it is worth saying. One week is not a streak, it is a week,
// and congratulating somebody for it reads as a machine trying to be
// encouraging.
func streakSentence(trend strava.Trend) string {
	if trend.StreakWeeks < 2 {
		return ""
	}
	return fmt.Sprintf("%d weeks running without a gap.", trend.StreakWeeks)
}

// ---------------------------------------------------------------------------
// The chart.

// trendChartOption builds the ECharts option for the training chart.
//
// The bands walk strava.Families, so the chart cannot show a colour for a
// family the server does not know about, or quietly omit one it does. The
// colours are the same --north-sport-* tokens the legend uses, which
// web/fitness/palette_test.go pins to the stylesheet.
func trendChartOption(trend strava.Trend) ([]byte, error) {
	labels := make([]string, 0, len(trend.Days))
	for _, day := range trend.Days {
		labels = append(labels, day.Date.Format("2 Jan"))
	}

	bands := make([]viz.TrendBand, 0, len(strava.Families))
	for _, family := range strava.Families {
		values := make([]float64, len(trend.Days))
		for i, day := range trend.Days {
			for _, session := range day.Sessions {
				if session.Family == family {
					values[i] += session.Minutes()
				}
			}
		}
		bands = append(bands, viz.TrendBand{
			Label:  family.Label(),
			Color:  sportColorVar(family),
			Values: round1s(values),
		})
	}

	return viz.TrainingTrendJSON(labels, bands, "Your normal", round1s(trend.Normal))
}

// sportColorVar is the CSS custom property holding a family's colour.
//
// Written out rather than built from the family name, for the same reason the
// legend's Tailwind classes are: a token name built at runtime cannot be
// checked, and one that does not exist renders as nothing, with no error
// anywhere and nothing to grep for.
func sportColorVar(f strava.SportFamily) string {
	switch f {
	case strava.FamilyRun:
		return "var(--north-sport-run)"
	case strava.FamilyRide:
		return "var(--north-sport-ride)"
	case strava.FamilySwim:
		return "var(--north-sport-swim)"
	case strava.FamilyWalk:
		return "var(--north-sport-walk)"
	case strava.FamilyStrength:
		return "var(--north-sport-strength)"
	default:
		return "var(--north-sport-other)"
	}
}

func round1s(in []float64) []float64 {
	out := make([]float64, len(in))
	for i, v := range in {
		out[i] = float64(int(v*10+0.5)) / 10
	}
	return out
}

// ---------------------------------------------------------------------------
// Figures.

// humanMinutes is a duration the way somebody says it. The chart's axis is in
// minutes because that is what one bar holds; a week's total is hours.
func humanMinutes(minutes float64) string {
	total := int(minutes + 0.5)
	h, m := total/60, total%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dh %02dm", h, m)
	}
}

func totalDistanceKm(trend strava.Trend) float64 { return trend.DistanceM / 1000 }

// ---------------------------------------------------------------------------
// The session list.

// routeSummary is one session's numbers, in the order they are read: how far,
// how long, how fast, how much up.
func routeSummary(session strava.Session) string {
	out := formatDuration(session.MovingTimeS)
	if session.DistanceM > 0 {
		out = fmt.Sprintf("%.1f km · %s", session.DistanceM/1000, out)
		if pace := paceMinPerKm(session); pace != "" {
			out += " · " + pace
		}
	}
	if session.ElevationM > 0 {
		out += fmt.Sprintf(" · %.0f m", session.ElevationM)
	}
	return out
}

func formatDuration(seconds int) string {
	d := time.Duration(seconds) * time.Second
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func paceMinPerKm(session strava.Session) string {
	if session.DistanceM <= 0 || session.MovingTimeS <= 0 {
		return ""
	}
	pace := (float64(session.MovingTimeS) / 60) / (session.DistanceM / 1000)
	if pace <= 0 {
		return ""
	}
	minutes := int(pace)
	seconds := int((pace - float64(minutes)) * 60)
	return fmt.Sprintf("%d:%02d /km", minutes, seconds)
}

// stravaActivityURL is where a session can be read in full. Empty for an
// activity with no Strava id, which is what a hand-built Session has.
func stravaActivityURL(session strava.Session) string {
	if session.StravaID <= 0 {
		return ""
	}
	return fmt.Sprintf("https://www.strava.com/activities/%d", session.StravaID)
}

// routeWhen is when a session happened, as the list reads it: the day, then
// the time. The flat list has no day heading to inherit a date from, so every
// row carries its own.
func routeWhen(session strava.Session, loc *time.Location) string {
	if session.StartedAt.IsZero() {
		return ""
	}
	if loc == nil {
		loc = time.UTC
	}
	return session.StartedAt.In(loc).Format("Mon 2 Jan 15:04")
}

// routeDayKey is the calendar day a session belongs to, in the reader's zone.
func routeDayKey(session strava.Session, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	if session.StartedAt.IsZero() {
		return ""
	}
	return session.StartedAt.In(loc).Format(time.DateOnly)
}

// sessionsPageURL is where a page of the list lives as a whole document, and
// sessionsFragmentURL is the same page as the markup the pager swaps in.
//
// Two URLs rather than one because they answer different readers: the href is
// a real page somebody can bookmark or open without JavaScript, and the hx-get
// is the fragment that leaves the chart above it standing.
func sessionsPageURL(page int) string {
	return "/app/fitness/activities?" + pageQuery(page)
}

func sessionsFragmentURL(page int) string {
	return "/app/fitness/activities/sessions?" + pageQuery(page)
}

func pageQuery(page int) string {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	return q.Encode()
}

// ellipsis is the gap in a page list, as a page number no page can have.
const ellipsis = 0

// sessionPageNumbers is the pager's run of numbers: the first page, the last
// page, the ones either side of the current one, and an ellipsis wherever that
// leaves a gap.
//
// Built here rather than in the template because it is a loop with three edge
// cases, and a template is the wrong place to read one.
func sessionPageNumbers(page strava.SessionPage) []int {
	if page.TotalPages <= 1 {
		return nil
	}

	// Few enough to show whole: a pager that elides two numbers is just a
	// pager that made itself harder to read.
	const showAll = 7
	if page.TotalPages <= showAll {
		out := make([]int, 0, page.TotalPages)
		for n := 1; n <= page.TotalPages; n++ {
			out = append(out, n)
		}
		return out
	}

	want := map[int]bool{1: true, page.TotalPages: true}
	for n := page.Page - 1; n <= page.Page+1; n++ {
		if n >= 1 && n <= page.TotalPages {
			want[n] = true
		}
	}

	out := make([]int, 0, len(want)+2)
	previous := 0
	for n := 1; n <= page.TotalPages; n++ {
		if !want[n] {
			continue
		}
		if previous != 0 && n != previous+1 {
			out = append(out, ellipsis)
		}
		out = append(out, n)
		previous = n
	}
	return out
}
