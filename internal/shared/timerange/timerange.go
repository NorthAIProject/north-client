// Package timerange turns a URL query value into a concrete window of time in
// the reader's own timezone.
//
// Every range-aware page in the application asks the same question — "which
// span of days am I looking at, and what did the span before it look like" —
// and every one of them would otherwise answer it slightly differently. Getting
// this wrong is invisible: a window computed in UTC for someone in Auckland
// silently reports the wrong day.
package timerange

import (
	"fmt"
	"time"
)

// Grain is the bucket width a range is charted at. A single day is read hour by
// hour; a quarter would be unreadable that way.
type Grain int

const (
	GrainHour Grain = iota
	GrainDay
	GrainWeek
)

// Keys, in the order the selector shows them.
const (
	KeyToday     = "today"
	KeyYesterday = "yesterday"
	KeyWeek      = "week"
	KeyMonth     = "month"
	KeyQuarter   = "quarter"
)

// DefaultKey is what an absent or unrecognised query value resolves to.
const DefaultKey = KeyToday

// Range is a half-open window [Since, Until) anchored to a location.
//
// Half-open is what makes the arithmetic safe: a day is midnight to the next
// midnight, so consecutive ranges tile without overlapping and without the
// last-nanosecond-of-the-day problem that inclusive ends always bring.
type Range struct {
	Key   string
	Label string
	Since time.Time
	Until time.Time
	Grain Grain

	loc *time.Location
}

// Parse resolves a query value into a range. It never fails: an unknown key is
// today, because a hand-edited `?range=` in the address bar must not be able to
// take a page down.
func Parse(q string, loc *time.Location) Range {
	if loc == nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	today := startOfDay(now)

	switch q {
	case KeyYesterday:
		yesterday := today.AddDate(0, 0, -1)
		return Range{
			Key:   KeyYesterday,
			Label: "Yesterday",
			Since: yesterday,
			Until: today,
			Grain: GrainHour,
			loc:   loc,
		}
	case KeyWeek:
		return Range{
			Key:   KeyWeek,
			Label: "Last 7 days",
			Since: today.AddDate(0, 0, -6),
			Until: today.AddDate(0, 0, 1),
			Grain: GrainDay,
			loc:   loc,
		}
	case KeyMonth:
		return Range{
			Key:   KeyMonth,
			Label: "Last 30 days",
			Since: today.AddDate(0, 0, -29),
			Until: today.AddDate(0, 0, 1),
			Grain: GrainDay,
			loc:   loc,
		}
	case KeyQuarter:
		return Range{
			Key:   KeyQuarter,
			Label: "Last 90 days",
			Since: today.AddDate(0, 0, -89),
			Until: today.AddDate(0, 0, 1),
			Grain: GrainWeek,
			loc:   loc,
		}
	default:
		return Range{
			Key:   KeyToday,
			Label: "Today",
			Since: today,
			Until: today.AddDate(0, 0, 1),
			Grain: GrainHour,
			loc:   loc,
		}
	}
}

// Between is a window the selector cannot express: an explicit half-open span
// somebody else already decided on.
//
// The weekly review is the reason it exists. A report covers the Monday–Monday
// week it was filed for, which may be months old, so Parse — which resolves
// every key relative to now — cannot name it. The location comes from since,
// because that is where the caller anchored the window; passing a bare UTC
// instant here would silently shift the day boundaries.
//
// Key is empty on purpose. A range with no selector key cannot be round-tripped
// through a URL, and pretending otherwise would put a link on a page that
// resolves to the wrong week.
func Between(since, until time.Time) Range {
	loc := since.Location()
	if loc == nil {
		loc = time.UTC
	}
	since = since.In(loc)
	until = until.In(loc)

	return Range{
		Label: fmt.Sprintf("%s – %s",
			since.Format("2 Jan"), until.Add(-time.Second).Format("2 Jan 2006")),
		Since: since,
		Until: until,
		Grain: GrainDay,
		loc:   loc,
	}
}

// All is every range the selector offers, resolved in one location.
func All(loc *time.Location) []Range {
	keys := []string{KeyToday, KeyYesterday, KeyWeek, KeyMonth, KeyQuarter}
	out := make([]Range, len(keys))
	for i, k := range keys {
		out[i] = Parse(k, loc)
	}
	return out
}

// Location is the timezone the window was resolved in.
func (r Range) Location() *time.Location {
	if r.loc == nil {
		return time.UTC
	}
	return r.loc
}

// Contains reports whether an instant falls inside the window.
func (r Range) Contains(t time.Time) bool {
	local := t.In(r.Location())
	return !local.Before(r.Since) && local.Before(r.Until)
}

// Days is the number of calendar days the window spans. Counted in calendar
// days rather than by dividing the duration, so a window crossing a DST change
// is still seven days and not six-and-23-hours.
func (r Range) Days() int {
	since := startOfDay(r.Since)
	until := startOfDay(r.Until)
	n := 0
	for d := since; d.Before(until); d = d.AddDate(0, 0, 1) {
		n++
	}
	if n == 0 {
		return 1
	}
	return n
}

// Previous is the window of equal length immediately before this one. It is
// what every delta on the dashboard is measured against.
func (r Range) Previous() Range {
	days := r.Days()
	prev := Range{
		Key:   r.Key,
		Label: "Previous " + r.Label,
		Since: r.Since.AddDate(0, 0, -days),
		Until: r.Since,
		Grain: r.Grain,
		loc:   r.loc,
	}
	return prev
}

// Bucket is one column on a chart's x-axis.
type Bucket struct {
	Label string
	Start time.Time
	End   time.Time
}

// Buckets divides the window at its grain. The result is the chart's x-axis:
// callers fill it by walking their rows once and dropping each into the bucket
// that Contains it, so a day with no data renders as a gap rather than
// disappearing and shifting every label after it.
func (r Range) Buckets() []Bucket {
	switch r.Grain {
	case GrainHour:
		return r.hourBuckets()
	case GrainWeek:
		return r.weekBuckets()
	default:
		return r.dayBuckets()
	}
}

func (r Range) hourBuckets() []Bucket {
	out := make([]Bucket, 0, 24)
	for t := r.Since; t.Before(r.Until); t = t.Add(time.Hour) {
		out = append(out, Bucket{
			Label: t.Format("15:04"),
			Start: t,
			End:   t.Add(time.Hour),
		})
	}
	return out
}

func (r Range) dayBuckets() []Bucket {
	out := make([]Bucket, 0, r.Days())
	for d := startOfDay(r.Since); d.Before(r.Until); d = d.AddDate(0, 0, 1) {
		out = append(out, Bucket{
			Label: d.Format("2 Jan"),
			Start: d,
			End:   d.AddDate(0, 0, 1),
		})
	}
	return out
}

func (r Range) weekBuckets() []Bucket {
	out := make([]Bucket, 0, r.Days()/7+1)
	for d := startOfDay(r.Since); d.Before(r.Until); d = d.AddDate(0, 0, 7) {
		end := d.AddDate(0, 0, 7)
		if end.After(r.Until) {
			end = r.Until
		}
		out = append(out, Bucket{
			Label: d.Format("2 Jan"),
			Start: d,
			End:   end,
		})
	}
	return out
}

// Index returns the bucket an instant belongs to, or -1 when it falls outside
// the window. Callers bucketing a slice of rows should use this rather than
// comparing against every bucket in turn.
func (r Range) Index(buckets []Bucket, t time.Time) int {
	local := t.In(r.Location())
	for i, b := range buckets {
		if !local.Before(b.Start) && local.Before(b.End) {
			return i
		}
	}
	return -1
}

// Labels is the x-axis of a chart built from this range.
func (r Range) Labels() []string {
	buckets := r.Buckets()
	out := make([]string, len(buckets))
	for i, b := range buckets {
		out[i] = b.Label
	}
	return out
}

// String is for logs and test failures.
func (r Range) String() string {
	return fmt.Sprintf("%s[%s..%s)", r.Key,
		r.Since.Format(time.RFC3339), r.Until.Format(time.RFC3339))
}

// startOfDay is midnight local time. Constructed from the calendar fields
// rather than by truncating, because truncation works in absolute time and a
// day is not always 24 hours long.
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
