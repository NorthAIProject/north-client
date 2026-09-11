package fitness

import (
	"math"
	"time"

	"github.com/NorthAIProject/north-client/internal/fitness/strava"
)

// The shape the terrain travels to the browser in.
//
// A deliberate subset, like the payload it replaces: the scene needs a date,
// a height, a colour and the routes it draws on the cap. Average speed,
// elapsed time, calories and the raw MET code all stay on the server, because
// shipping somebody's whole training history to the client so that a canvas
// can ignore most of it is not a trade worth making.
//
// Dates cross as local calendar strings, never as instants. The server has
// already done the timezone work, deciding which local day each activity
// belongs to; sending a timestamp would let the client re-derive a day and
// get a different answer, which is the bug the whole design exists to avoid.

// TerrainPayload is one page of terrain.
type TerrainPayload struct {
	Weeks []TerrainWeekPayload `json:"weeks"`

	// HasOlder says whether travelling further back will find anything. When
	// it is false the scene stops asking rather than requesting pages that
	// will always come back empty.
	HasOlder bool   `json:"has_older"`
	OldestAt string `json:"oldest_at,omitempty"`

	// LoadScaleMETMin is the height the tallest ordinary column stands for.
	// Sent on the first page only; the client pins it for the session, so the
	// landscape does not rescale itself as pages arrive.
	LoadScaleMETMin float64 `json:"load_scale_met_min,omitempty"`

	// NextBefore is the cursor for the page after this one, ready to use.
	// Letting the client work out "the Monday before the oldest week I have"
	// would be a second implementation of the calendar, in the language that
	// does not own it.
	NextBefore string `json:"next_before,omitempty"`
}

type TerrainWeekPayload struct {
	Start      string              `json:"start"`
	Label      string              `json:"label"`
	LoadMETMin float64             `json:"load_met_min"`
	Days       []TerrainDayPayload `json:"days"`
}

type TerrainDayPayload struct {
	Date    string `json:"date"`
	Weekday int    `json:"weekday"`
	Label   string `json:"label"`

	LoadMETMin  float64 `json:"load_met_min"`
	Sessions    int     `json:"sessions"`
	DistanceM   float64 `json:"distance_m"`
	ElevationM  float64 `json:"elevation_m"`
	MovingTimeS int     `json:"moving_time_s"`

	Dominant string                `json:"dominant"`
	Mixed    bool                  `json:"mixed"`
	Mix      []TerrainSharePayload `json:"mix,omitempty"`

	Routes []TerrainRoutePayload `json:"routes,omitempty"`
}

type TerrainSharePayload struct {
	Family     string  `json:"family"`
	LoadMETMin float64 `json:"load_met_min"`
}

type TerrainRoutePayload struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Sport  string `json:"sport"`
	Family string `json:"family"`

	Polyline    string  `json:"polyline"`
	DistanceM   float64 `json:"distance_m"`
	ElevationM  float64 `json:"elevation_m"`
	MovingTimeS int     `json:"moving_time_s"`
	LoadMETMin  float64 `json:"load_met_min"`

	// StartedAt is a wall clock, not an instant: enough to order a day's
	// sessions in a tooltip, not enough to reconstruct a moment in time.
	StartedAt string `json:"started_at"`

	// HasStreams is the seam for the elevation ribbons that come later. False
	// everywhere today; the client already branches on it, so turning ribbons
	// on is a server change rather than a change to the shape of the wire.
	HasStreams bool `json:"has_streams"`
}

// NewTerrainPayload converts the domain terrain into the wire shape.
func NewTerrainPayload(page strava.TerrainPage) TerrainPayload {
	out := TerrainPayload{
		Weeks:           make([]TerrainWeekPayload, 0, len(page.Weeks)),
		HasOlder:        page.HasOlder,
		LoadScaleMETMin: page.LoadScaleMETMin,
	}
	if page.OldestAt != nil {
		out.OldestAt = page.OldestAt.Format(time.DateOnly)
	}
	if page.HasOlder && len(page.Weeks) > 0 {
		out.NextBefore = page.Weeks[0].Start.Format(time.DateOnly)
	}

	for _, week := range page.Weeks {
		w := TerrainWeekPayload{
			Start:      week.Start.Format(time.DateOnly),
			Label:      week.Start.Format("2 Jan"),
			LoadMETMin: round1(week.LoadMETMin),
			Days:       make([]TerrainDayPayload, 0, len(week.Days)),
		}

		for _, day := range week.Days {
			w.Days = append(w.Days, newDayPayload(day))
		}
		out.Weeks = append(out.Weeks, w)
	}
	return out
}

func newDayPayload(day strava.TerrainDay) TerrainDayPayload {
	d := TerrainDayPayload{
		Date:        day.Date.Format(time.DateOnly),
		Weekday:     weekdayIndex(day.Weekday),
		Label:       day.Date.Format("Mon 2 Jan"),
		LoadMETMin:  round1(day.LoadMETMin),
		Sessions:    day.Sessions,
		DistanceM:   round1(day.DistanceM),
		ElevationM:  round1(day.ElevationM),
		MovingTimeS: day.MovingTimeS,
		Dominant:    string(day.Dominant),
		Mixed:       day.Mixed,
	}

	// A rest day carries no mix and no routes. Sending empty arrays for the
	// majority of days in most accounts is a lot of bytes to say nothing.
	if day.Rest() {
		return d
	}

	for _, share := range day.Mix {
		d.Mix = append(d.Mix, TerrainSharePayload{
			Family:     string(share.Family),
			LoadMETMin: round1(share.LoadMETMin),
		})
	}
	for _, route := range day.Routes {
		d.Routes = append(d.Routes, TerrainRoutePayload{
			ID:          route.StravaID,
			Name:        route.Name,
			Sport:       route.Sport,
			Family:      string(route.Family),
			Polyline:    route.Polyline,
			DistanceM:   round1(route.DistanceM),
			ElevationM:  round1(route.ElevationM),
			MovingTimeS: route.MovingTimeS,
			LoadMETMin:  round1(route.LoadMETMin),
			StartedAt:   route.StartedAt.Format("15:04"),
			HasStreams:  route.HasStreams,
		})
	}
	return d
}

// weekdayIndex is Monday-first, matching the order the days are laid out in.
// Go's own Weekday puts Sunday at 0, which would draw the week starting on
// the wrong column.
func weekdayIndex(w time.Weekday) int {
	return (int(w) + 6) % 7
}

// round1 keeps one decimal place. The scene cannot draw more precision than
// that and the numbers are read, not recomputed; full float64s would add a
// long tail of noise to every payload for nothing.
func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
