package strava

import "time"

// Session is one activity, as the interface reads it.
//
// It was TerrainRoute, a shape built for a 3D scene: it carried a route
// polyline to draw on a column cap, a HasStreams flag described as the seam
// for elevation ribbons, and a load in MET-minutes to set the column's height.
// The scene is gone and none of those survived it — HasStreams was never once
// set true in its whole life, and MET-minutes turned out to be the reason
// nobody could read the page. What is left is what somebody actually wants to
// know about a session: when it was, what it was, how far, how long, how much
// climbing.
type Session struct {
	StravaID int64
	Name     string
	Sport    string
	Family   SportFamily

	DistanceM   float64
	ElevationM  float64
	MovingTimeS int
	StartedAt   time.Time
}

// Minutes is the session's moving time, which is the unit the chart is drawn
// in and the one the interface speaks.
//
// Moving time rather than elapsed: a ride that included a half-hour café stop
// did not cost half an hour of training, and counting it would make a long
// lunch look like a hard week.
func (s Session) Minutes() float64 { return float64(s.MovingTimeS) / 60 }

// sessionOf is one stored activity as the shape the interface reads.
//
// Shared by the trend and by the session list rather than written twice: two
// mappings of the same row drift, and the one that drifts is always the one
// with no test pinning it to the other.
func sessionOf(a Activity, loc *time.Location) Session {
	if loc == nil {
		loc = time.UTC
	}
	return Session{
		StravaID:    a.StravaID,
		Name:        a.Name,
		Sport:       a.SportType,
		Family:      Family(a.SportType, ""),
		DistanceM:   a.DistanceM,
		ElevationM:  a.ElevationGainM,
		MovingTimeS: a.MovingTimeS,
		StartedAt:   a.StartDate.In(loc),
	}
}
