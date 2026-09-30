package util

import "time"

// WholeDaysBetween returns the number of complete 24-hour periods from `from` to `to`, or 0
// when `to` is not after `from`. This is absolute elapsed time rather than calendar days, so it
// is unaffected by DST transitions.
func WholeDaysBetween(from, to time.Time) int {
	if !to.After(from) {
		return 0
	}
	return int(to.Sub(from) / (24 * time.Hour))
}

// WholeMonthsBetween returns the number of complete calendar months from `from` to `to`, or 0
// when `to` is not after `from`. A month completes once the same day-of-month and time-of-day is
// reached. When `from` is on a day the target month lacks, the anniversary clamps to that
// month's last day, so 31 Jan -> 28 Feb is one whole month.
//
// Months are counted in UTC, and both arguments are converted before anything is compared.
// Unlike WholeDaysBetween this cannot be done on the instants alone: the month and year are read
// off the calendar, which is location-dependent. Reading them in different zones can put the
// initial estimate a month too low, and the correction below only ever decrements — so a mixed
// pair would silently under-count. Normalising both to one zone is what makes that correction
// sound: within a single zone the estimate is never too low, and at most one month too high.
func WholeMonthsBetween(from, to time.Time) int {
	if !to.After(from) {
		return 0
	}

	from, to = from.UTC(), to.UTC()

	months := int(to.Month()) - int(from.Month()) + 12*(to.Year()-from.Year())
	if months <= 0 {
		return 0
	}

	if to.Before(addMonthsClamped(from, months)) {
		months--
	}
	if months < 0 {
		return 0
	}
	return months
}

// addMonthsClamped clamps the day to the target month's last day instead of overflowing into
// the following month the way time.AddDate does.
func addMonthsClamped(t time.Time, months int) time.Time {
	year, month, day := t.Date()

	totalMonths := int(month) - 1 + months
	targetYear := year + totalMonths/12
	targetMonth := time.Month(totalMonths%12 + 1)

	if lastDay := daysInMonth(targetYear, targetMonth); day > lastDay {
		day = lastDay
	}

	hour, minute, second := t.Clock()
	return time.Date(targetYear, targetMonth, day, hour, minute, second, t.Nanosecond(), t.Location())
}

func daysInMonth(year int, month time.Month) int {
	// Day 0 of the following month is the last day of this one.
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
