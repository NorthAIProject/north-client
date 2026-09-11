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
		if !day.Rest() {
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

func activeDays(page strava.TerrainPage) int {
	var n int
	for _, week := range page.Weeks {
		for _, day := range week.Days {
			if !day.Rest() {
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
