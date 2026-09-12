package fitness

import (
	"fmt"
	"net/url"
	"time"

	"github.com/NorthAIProject/north-client/internal/fitness/strava"
)

// terrainState is which of the page's five shapes to render.
//
// An explicit state rather than a chain of if/else in the template. The old
// page had four conditions and only three branches: Unavailable was set by the
// handler, documented in a comment as making the template "say the activities
// could not be read", and never tested for — so a failed read rendered as
// "nothing imported yet", telling someone their history was empty when it was
// merely unreadable.
type terrainStateKind int

const (
	stateTerrain terrainStateKind = iota
	stateUnconfigured
	stateDisconnected
	stateUnavailable
	stateEmpty
)

func terrainState(status strava.Status, page strava.TerrainPage) terrainStateKind {
	switch {
	case !status.Configured:
		return stateUnconfigured
	case !status.Connected:
		return stateDisconnected
	case status.Unavailable:
		return stateUnavailable
	case !hasAnySession(page):
		return stateEmpty
	default:
		return stateTerrain
	}
}

func hasAnySession(page strava.TerrainPage) bool {
	for _, week := range page.Weeks {
		if weekHasSessions(week) {
			return true
		}
	}
	// An account whose only history is older than this window still has
	// something to show, and the strip offers the way back to it.
	return page.HasOlder
}

func weekHasSessions(week strava.TerrainWeek) bool {
	for _, day := range week.Days {
		if day.Sessions > 0 {
			return true
		}
	}
	return false
}

// currentWeekLoad is the load of the most recent week drawn, which is the one
// the reader is standing in.
func currentWeekLoad(page strava.TerrainPage) float64 {
	if len(page.Weeks) == 0 {
		return 0
	}
	return page.Weeks[len(page.Weeks)-1].LoadMETMin
}

func totalSessions(page strava.TerrainPage) int {
	var n int
	for _, week := range page.Weeks {
		for _, day := range week.Days {
			n += day.Sessions
		}
	}
	return n
}

func totalDistanceKm(page strava.TerrainPage) float64 {
	var m float64
	for _, week := range page.Weeks {
		for _, day := range week.Days {
			m += day.DistanceM
		}
	}
	return m / 1000
}

func totalClimbM(page strava.TerrainPage) float64 {
	var m float64
	for _, week := range page.Weeks {
		for _, day := range week.Days {
			m += day.ElevationM
		}
	}
	return m
}

// activeDays counts the days something was recorded on.
//
// Deliberately not !Rest(): a day that has not happened is neither rest nor
// active, and counting it as active would report a week as busier than it was
// every time the page is opened before Sunday.
func activeDays(page strava.TerrainPage) int {
	var n int
	for _, week := range page.Weeks {
		for _, day := range week.Days {
			if day.Sessions > 0 {
				n++
			}
		}
	}
	return n
}

// countedDays is the denominator beside activeDays: the days that have had a
// chance to happen. Counting the whole grid would make every week read as
// worse than it was until it ended.
func countedDays(page strava.TerrainPage) int {
	var n int
	for _, week := range page.Weeks {
		for _, day := range week.Days {
			if !day.Future {
				n++
			}
		}
	}
	return n
}

// olderURL is the link to the page of weeks before this one.
//
// Built here rather than in the template so the cursor format lives next to
// the code that parses it.
func olderURL(page strava.TerrainPage) string {
	if len(page.Weeks) == 0 {
		return "/app/fitness/activities"
	}
	q := url.Values{}
	q.Set("before", page.Weeks[0].Start.Format(time.DateOnly))
	return "/app/fitness/activities/list?" + q.Encode()
}

// routeSummary is one session's numbers, in the order they are read: how far,
// how long, how fast, how much up.
func routeSummary(route strava.TerrainRoute) string {
	out := formatDuration(route.MovingTimeS)
	if route.DistanceM > 0 {
		out = fmt.Sprintf("%.1f km · %s", route.DistanceM/1000, out)
		if pace := paceMinPerKm(route); pace != "" {
			out += " · " + pace
		}
	}
	if route.ElevationM > 0 {
		out += fmt.Sprintf(" · %.0f m", route.ElevationM)
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

func paceMinPerKm(route strava.TerrainRoute) string {
	if route.DistanceM <= 0 || route.MovingTimeS <= 0 {
		return ""
	}
	pace := (float64(route.MovingTimeS) / 60) / (route.DistanceM / 1000)
	if pace <= 0 {
		return ""
	}
	minutes := int(pace)
	seconds := int((pace - float64(minutes)) * 60)
	return fmt.Sprintf("%d:%02d /km", minutes, seconds)
}

// weeksNewestFirst is page.Weeks in the order a reader travels them.
//
// The terrain is laid out oldest-first — scene.js pins offset 0 to the newest
// week so that prepending an older page moves nothing already drawn — but a
// list that grows downward as older pages arrive only reads correctly running
// newest to oldest. Reversed into a copy rather than in place: the same page
// goes to the scene's JSON payload, and reversing that would put the landscape
// back to front.
func weeksNewestFirst(page strava.TerrainPage) []strava.TerrainWeek {
	out := make([]strava.TerrainWeek, len(page.Weeks))
	for i, week := range page.Weeks {
		out[len(page.Weeks)-1-i] = week
	}
	return out
}

// daysNewestFirst is a week's days, latest first, for the same reason.
func daysNewestFirst(week strava.TerrainWeek) []strava.TerrainDay {
	out := make([]strava.TerrainDay, 0, len(week.Days))
	for i := len(week.Days) - 1; i >= 0; i-- {
		out = append(out, week.Days[i])
	}
	return out
}

// dayKey is how a day is named on the wire between the list and the scene.
func dayKey(day strava.TerrainDay) string {
	return day.Date.Format(time.DateOnly)
}

// dayElementID is the day's anchor in the document.
//
// The scene scrolls the list to the day somebody clicked. It used to find the
// row by searching the markup for an Alpine attribute containing the date,
// which tied a behaviour to the spelling of a template. An id is the thing
// that survives the next edit to this file.
func dayElementID(day strava.TerrainDay) string {
	return "day-" + dayKey(day)
}

// routeClock is when a session started, in the reader's own zone. Empty when
// the import carried no start time, so the row omits the column rather than
// claiming midnight.
func routeClock(route strava.TerrainRoute, loc *time.Location) string {
	if route.StartedAt.IsZero() {
		return ""
	}
	if loc == nil {
		loc = time.UTC
	}
	return route.StartedAt.In(loc).Format("15:04")
}

// stravaActivityURL is where a session can be read in full. Empty for an
// activity with no Strava id, which is what a manually built TerrainRoute has.
func stravaActivityURL(route strava.TerrainRoute) string {
	if route.StravaID <= 0 {
		return ""
	}
	return fmt.Sprintf("https://www.strava.com/activities/%d", route.StravaID)
}
