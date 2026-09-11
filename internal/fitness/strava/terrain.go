package strava

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

// The calendar terrain.
//
// The activity view used to lay sessions out on a square-ish grid, which
// carried no information: the two ground axes were arrangement, and only the
// vertical one said anything. Here the ground carries time — seven columns of
// weekday, one row per week, receding into the past — so a position in the
// scene is a date, and a gap in the landscape is a week somebody rested.
//
// The vertical axis is training load in MET-minutes. That choice is what lets
// a gym session and a run stand next to each other: elevation gain is zero for
// most of a real week, and duration alone says a long walk outranks an
// interval session. MET-minutes is not a perfect measure of training stress —
// it knows nothing about intensity distribution or heart rate — which is why
// the interface labels the axis "MET-minutes" rather than "training load", and
// why the wire field is named load_met_min rather than something generic that
// a better measure would have to pretend to be.

const (
	// daysPerWeek is spelled out because [7]TerrainDay appears in several
	// places and a bare 7 in the bucketing loop reads as a magic number.
	daysPerWeek = 7

	// loadScaleWindowWeeks is how far back the vertical scale is measured
	// over. Half a year: long enough that a hard fortnight does not redefine
	// what "tall" means, short enough that a fitness change from a year ago
	// is not still flattening this month.
	loadScaleWindowWeeks = 26

	// loadScalePercentile puts the top of the scale at the 90th percentile of
	// active days rather than at the largest one.
	//
	// The largest is the wrong anchor. One six-hour hike is an order of
	// magnitude above a normal session, and scaling to it turns every other
	// day into a pancake — which is the exact failure the elevation-based
	// version of this scene already had, where the majority of a week sat
	// flat on the ground plane.
	loadScalePercentile = 0.90

	// defaultTerrainWeeks is one page of terrain: two months, which fills the
	// view without the far end being too small to read.
	defaultTerrainWeeks = 8

	// maxTerrainWeeks bounds what one request may ask for, so a hand-edited
	// URL cannot ask the database for a decade in one go.
	maxTerrainWeeks = 12

	// minLoadScale keeps the scale meaningful for an account with almost no
	// history. Without a floor, a single ten-minute walk becomes the ceiling
	// and the terrain reads as a full week of maximum effort.
	minLoadScale = 120.0
)

// SportShare is one family's contribution to a day's load.
type SportShare struct {
	Family     SportFamily
	LoadMETMin float64
}

// TerrainRoute is one session, as the scene draws it.
//
// StravaID and HasStreams are the seam for the elevation ribbons that come
// later: a stream request names an activity by its Strava id, and the client
// already branches on HasStreams, so turning ribbons on is a server change
// rather than a change to the shape of the wire.
type TerrainRoute struct {
	StravaID int64
	Name     string
	Sport    string
	Family   SportFamily

	Polyline    string
	DistanceM   float64
	ElevationM  float64
	MovingTimeS int
	LoadMETMin  float64
	StartedAt   time.Time

	HasStreams bool
}

// TerrainDay is one column of the terrain.
//
// Every day in the window gets one, including the days nothing happened. A
// rest day is drawn as a flat plate rather than omitted: a week with holes in
// it is a lie about the week, and the flat ground is what makes the tall
// columns mean anything.
type TerrainDay struct {
	Date    time.Time
	Weekday time.Weekday

	LoadMETMin  float64
	Sessions    int
	DistanceM   float64
	ElevationM  float64
	MovingTimeS int

	Dominant SportFamily
	Mixed    bool
	Mix      []SportShare

	Routes []TerrainRoute
}

// Rest reports whether nothing was recorded on this day.
func (d TerrainDay) Rest() bool { return d.Sessions == 0 }

// TerrainWeek is one row of the terrain, Monday first.
type TerrainWeek struct {
	Start      time.Time
	Days       [daysPerWeek]TerrainDay
	LoadMETMin float64
}

// TerrainPage is one request's worth of terrain, plus the two facts the
// scrolling needs: whether to keep asking, and where the ground ends.
type TerrainPage struct {
	Weeks    []TerrainWeek
	HasOlder bool
	OldestAt *time.Time

	// LoadScaleMETMin pins the vertical axis for the whole session.
	//
	// Sent on the first page and never recomputed, because a scale derived
	// per page would silently rescale the landscape as pages arrive: two
	// columns of equal height would then mean different loads, which is worse
	// than a scale that is merely imperfect.
	LoadScaleMETMin float64
}

// dominanceThreshold is the share of a day's load one family needs before the
// day is drawn as that family's colour outright.
//
// Below it the day is Mixed, and the interface says so. A day that is 51% run
// and 49% lifting is not a run, and colouring it as one is a confident lie
// about what somebody did. Stacking the column into per-sport segments is the
// only fully honest answer; this is the second best.
const dominanceThreshold = 0.60

// loadMETMin is the training load of one activity, in MET-minutes.
//
// Moving time, not elapsed: a ride that includes a half-hour café stop did not
// cost anything for that half hour. This matches how the calorie estimate
// already reads the same activity.
func loadMETMin(a Activity) float64 {
	code, _ := MapSportType(a.SportType, "")
	met, ok := activity.LookupMET(code)
	if !ok {
		return 0
	}
	return met.Value * (float64(a.MovingTimeS) / 60.0)
}

// buildTerrain buckets activities into weeks of days, in loc.
//
// Pure, and deliberately separate from the service: every interesting case
// here is a calendar case — a day with no midnight, a week that is 167 hours
// long, an activity that belongs to a different day in a different zone — and
// none of them need a database to exercise.
//
// from and to are absolute instants; the caller has already decided which
// local weeks they are. The window is half-open, matching the query that
// produced the activities.
func buildTerrain(activities []Activity, loc *time.Location, from, to time.Time) []TerrainWeek {
	if loc == nil {
		loc = time.UTC
	}

	// Index by local calendar date. A map key of the formatted date rather
	// than the time itself: two time.Time values for the same midnight can
	// differ in monotonic clock reading and in location pointer, and neither
	// difference means they are different days.
	byDay := make(map[string][]Activity)
	for _, a := range activities {
		local := a.StartDate.In(loc)
		if local.Before(from.In(loc)) || !local.Before(to.In(loc)) {
			continue
		}
		key := timerange.StartOfDay(local).Format(time.DateOnly)
		byDay[key] = append(byDay[key], a)
	}

	var weeks []TerrainWeek
	for start := timerange.StartOfWeek(from.In(loc)); start.Before(to.In(loc)); {
		week := TerrainWeek{Start: start}

		for i := range daysPerWeek {
			y, m, d := start.Date()
			date := timerange.StartOfDay(time.Date(y, m, d+i, 12, 0, 0, 0, loc))

			day := buildDay(date, byDay[date.Format(time.DateOnly)])
			week.Days[i] = day
			week.LoadMETMin += day.LoadMETMin
		}

		weeks = append(weeks, week)

		y, m, d := start.Date()
		start = timerange.StartOfDay(time.Date(y, m, d+daysPerWeek, 12, 0, 0, 0, loc))
	}

	return weeks
}

func buildDay(date time.Time, activities []Activity) TerrainDay {
	day := TerrainDay{
		Date:     date,
		Weekday:  date.Weekday(),
		Dominant: FamilyOther,
	}
	if len(activities) == 0 {
		return day
	}

	// Oldest first within the day, so a tooltip reads a morning session before
	// an evening one.
	sort.SliceStable(activities, func(i, j int) bool {
		return activities[i].StartDate.Before(activities[j].StartDate)
	})

	byFamily := make(map[SportFamily]float64)
	movingByFamily := make(map[SportFamily]int)

	for _, a := range activities {
		family := Family(a.SportType, "")
		load := loadMETMin(a)

		day.Sessions++
		day.LoadMETMin += load
		day.DistanceM += a.DistanceM
		day.ElevationM += a.ElevationGainM
		day.MovingTimeS += a.MovingTimeS

		byFamily[family] += load
		movingByFamily[family] += a.MovingTimeS

		day.Routes = append(day.Routes, TerrainRoute{
			StravaID:    a.StravaID,
			Name:        a.Name,
			Sport:       a.SportType,
			Family:      family,
			Polyline:    a.SummaryPolyline,
			DistanceM:   a.DistanceM,
			ElevationM:  a.ElevationGainM,
			MovingTimeS: a.MovingTimeS,
			LoadMETMin:  load,
			StartedAt:   a.StartDate.In(date.Location()),
		})
	}

	day.Mix = sortedShares(byFamily, movingByFamily)
	day.Dominant, day.Mixed = dominant(day.Mix, movingByFamily, day.LoadMETMin)
	return day
}

// sortedShares orders a day's families by load, descending.
//
// Ties break by moving time and then by the order in Families, so the same day
// never colours itself differently on two page loads. Map iteration order in
// Go is deliberately random; without a total order this would flicker.
func sortedShares(byFamily map[SportFamily]float64, movingByFamily map[SportFamily]int) []SportShare {
	shares := make([]SportShare, 0, len(byFamily))
	for family, load := range byFamily {
		shares = append(shares, SportShare{Family: family, LoadMETMin: load})
	}

	rank := make(map[SportFamily]int, len(Families))
	for i, f := range Families {
		rank[f] = i
	}

	sort.Slice(shares, func(i, j int) bool {
		if shares[i].LoadMETMin != shares[j].LoadMETMin {
			return shares[i].LoadMETMin > shares[j].LoadMETMin
		}
		if movingByFamily[shares[i].Family] != movingByFamily[shares[j].Family] {
			return movingByFamily[shares[i].Family] > movingByFamily[shares[j].Family]
		}
		return rank[shares[i].Family] < rank[shares[j].Family]
	})
	return shares
}

// dominant picks the colour a day is drawn in, and whether that colour is the
// whole story.
func dominant(mix []SportShare, movingByFamily map[SportFamily]int, total float64) (SportFamily, bool) {
	if len(mix) == 0 {
		return FamilyOther, false
	}
	top := mix[0]

	// A day of zero-load sessions still happened and still has a colour. Fall
	// back to moving time, which is the only thing left to rank by.
	if total <= 0 {
		best := top.Family
		for _, share := range mix {
			if movingByFamily[share.Family] > movingByFamily[best] {
				best = share.Family
			}
		}
		return best, len(mix) > 1
	}

	return top.Family, top.LoadMETMin/total < dominanceThreshold
}

// loadScale is the value the tallest ordinary column stands for.
//
// Percentile over active days only. Including rest days would put the 90th
// percentile of a three-session week somewhere near zero, and every session
// would clip.
func loadScale(weeks []TerrainWeek) float64 {
	var active []float64
	for _, week := range weeks {
		for _, day := range week.Days {
			if day.LoadMETMin > 0 {
				active = append(active, day.LoadMETMin)
			}
		}
	}
	if len(active) == 0 {
		return minLoadScale
	}

	sort.Float64s(active)

	// Nearest-rank: the smallest value at or above the percentile. For a
	// single active day this is that day, which the floor below then rescues.
	index := int(math.Ceil(loadScalePercentile*float64(len(active)))) - 1
	index = min(max(index, 0), len(active)-1)

	return math.Max(active[index], minLoadScale)
}

// Terrain builds the calendar terrain for a window of weeks ending at before.
//
// A zero before means "up to now". The window runs backwards from there, so
// travelling into the past is a matter of passing the start of the oldest week
// already drawn.
//
// Reads North's own copy rather than calling Strava, like everything else on
// this page: opening it is fast, it works when Strava is down, and it costs
// nothing against the rate limit.
func (s *Service) Terrain(ctx context.Context, userID uuid.UUID, loc *time.Location, before time.Time, weeks int) (TerrainPage, error) {
	if loc == nil {
		loc = time.UTC
	}
	if weeks <= 0 || weeks > maxTerrainWeeks {
		weeks = defaultTerrainWeeks
	}

	// The window ends at the start of the week after the cursor, so the week
	// the cursor falls in is drawn whole rather than truncated at today.
	end := before
	if end.IsZero() {
		end = time.Now()
	}
	end = startOfNextWeek(end.In(loc))

	y, m, d := end.Date()
	start := timerange.StartOfDay(time.Date(y, m, d-daysPerWeek*weeks, 12, 0, 0, 0, loc))

	activities, err := s.repo.ActivitiesBetween(ctx, userID, start, end)
	if err != nil {
		return TerrainPage{}, err
	}

	page := TerrainPage{Weeks: buildTerrain(activities, loc, start, end)}

	oldest, err := s.repo.OldestBefore(ctx, userID, start)
	if err != nil {
		return TerrainPage{}, err
	}
	page.OldestAt = oldest
	page.HasOlder = oldest != nil

	// The scale is measured once, over its own window, and only for the first
	// page. Later pages inherit whatever the first one pinned.
	if before.IsZero() {
		scaleStart := timerange.StartOfDay(time.Date(y, m, d-daysPerWeek*loadScaleWindowWeeks, 12, 0, 0, 0, loc))

		scaleWeeks := page.Weeks
		if scaleStart.Before(start) {
			forScale, err := s.repo.ActivitiesBetween(ctx, userID, scaleStart, end)
			if err != nil {
				return TerrainPage{}, err
			}
			scaleWeeks = buildTerrain(forScale, loc, scaleStart, end)
		}
		page.LoadScaleMETMin = loadScale(scaleWeeks)
	}

	return page, nil
}

// startOfNextWeek is the Monday after the week t falls in.
func startOfNextWeek(t time.Time) time.Time {
	week := timerange.StartOfWeek(t)
	y, m, d := week.Date()
	return timerange.StartOfDay(time.Date(y, m, d+daysPerWeek, 12, 0, 0, 0, week.Location()))
}
