package fitness

import (
	"fmt"
	"net/url"
	"strconv"
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

// stravaActivityURL is where a session can be read in full. Empty for an
// activity with no Strava id, which is what a manually built TerrainRoute has.
func stravaActivityURL(route strava.TerrainRoute) string {
	if route.StravaID <= 0 {
		return ""
	}
	return fmt.Sprintf("https://www.strava.com/activities/%d", route.StravaID)
}

// routeWhen is when a session happened, as the list reads it: the day, then
// the time. The flat list has no day heading to inherit a date from, so every
// row carries its own.
func routeWhen(route strava.TerrainRoute, loc *time.Location) string {
	if route.StartedAt.IsZero() {
		return ""
	}
	if loc == nil {
		loc = time.UTC
	}
	return route.StartedAt.In(loc).Format("Mon 2 Jan 15:04")
}

// routeDayKey is the calendar day a session belongs to, in the reader's zone.
// It is how a row in the list names the column the scene should highlight.
func routeDayKey(route strava.TerrainRoute, loc *time.Location) string {
	if loc == nil {
		loc = time.UTC
	}
	if route.StartedAt.IsZero() {
		return ""
	}
	return route.StartedAt.In(loc).Format(time.DateOnly)
}

// sessionsPageURL is where a page of the list lives as a whole document, and
// sessionsFragmentURL is the same page as the markup the pager swaps in.
//
// Two URLs rather than one because they answer different readers: the href is
// a real page somebody can bookmark or open without JavaScript, and the hx-get
// is the fragment that leaves the WebGL scene standing.
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
