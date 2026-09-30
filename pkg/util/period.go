package util

import (
	"time"

	"github.com/rickb777/date/v2"
	dateperiod "github.com/rickb777/period"
)

var (
	monthMap = map[time.Month]int{
		time.January:   31,
		time.February:  28, // 29 with leap year
		time.March:     31,
		time.April:     30,
		time.May:       31,
		time.June:      30,
		time.July:      31,
		time.August:    31,
		time.September: 30,
		time.October:   31,
		time.November:  30,
		time.December:  31,
	}
)

func isLeapYear(year int) bool {
	// All leap years are divisible by 4.
	if year%4 != 0 {
		return false
	}
	// The years that are divisible by 4, but not by 100, are leap years.
	if year%100 != 0 {
		return true
	}
	// The years that are divisible by 100, but not by 400, are not leap years.
	if year%400 != 0 {
		return false
	}
	// However, the years divisible by 100 and 400 are leap years.
	return true
}

type Period struct {
	period *dateperiod.Period
}

func NewPeriod(periodStr string) (*Period, error) {
	if periodStr == "" {
		return nil, nil
	}

	p, err := dateperiod.Parse(periodStr)

	return &Period{period: &p}, err
}

func NewPeriodZero() *Period {
	p, _ := NewPeriod("P0")
	return p
}

func (p *Period) String() string {
	if p == nil || p.period == nil || p.period.IsZero() {
		return ""
	}
	return p.period.String()
}

func (p *Period) IsNegative() bool {
	return p.period.IsNegative()
}

func (p *Period) AddToTimeByDateEndInclusive(date time.Time) time.Time {
	return p.AddToTimeEndExclusive(date).AddDate(0, 0, -1)
}

func (p *Period) AddToTimeEndExclusive(date time.Time) time.Time {
	if p.period.Hours() == 0 && p.period.Minutes() == 0 && p.period.Seconds() == 0 {
		return p.addDatesToTime(date, p.period.Years(), p.period.Months(), p.period.DaysIncWeeks()).Truncate(24 * time.Hour) // make 00:00:00
	} else if p.period.Years() == 0 && p.period.Months() == 0 && p.period.Days() == 0 {
		return p.addTimeToTime(date)
	} else {
		// If we have both then we do adding the order of Y,M,D,H,M,S
		newT := p.addDatesToTime(date, p.period.Years(), p.period.Months(), p.period.DaysIncWeeks())
		return p.addTimeToTime(newT)
	}
}

// AddToDateEndInclusiveToTime returns the end-exclusive time for the period applied to a date.
// When the input day is the 31st and the target month has fewer days, the result overflows to
// the first of the next month so that the inclusive end equals the last day of the target month.
// Example: 2025-10-31 + P1M = 2025-12-01T00:00:00Z (exclusive), inclusive end = 2025-11-30.
// The useLegacy parameter allows choosing the legacy calculation method if true.
func (p *Period) AddToDateEndInclusiveToTime(loc *time.Location, in date.Date, useLegacy bool) time.Time {
	t := time.Date(in.Year(), in.Month(), in.Day(), 0, 0, 0, 0, loc)
	if useLegacy {
		return p.addDatesToTime(t, p.period.Years(), p.period.Months(), p.period.DaysIncWeeks())
	}
	return p.addDatesToTimeMonth31Overflow(t, p.period.Years(), p.period.Months(), p.period.DaysIncWeeks())
}

// AddToDateEndInclusive returns the last valid date (inclusive) for the period applied to a date.
// When the input day is the 31st and the target month has fewer days, the result is the last day
// of the target month.
func (p *Period) AddToDateEndInclusive(loc *time.Location, in date.Date) date.Date {
	t := time.Date(in.Year(), in.Month(), in.Day(), 0, 0, 0, 0, loc)
	newDate := p.addDatesToTimeMonth31Overflow(t, p.period.Years(), p.period.Months(), p.period.DaysIncWeeks())
	return date.New(newDate.Year(), newDate.Month(), newDate.Day()).AddDate(0, 0, -1)
}

// AddToTime We assume that both duration and time have a time, so then we always are end exclusive
func (p *Period) addTimeToTime(date time.Time) time.Time {
	return date.Add(time.Duration(p.period.Hours())*time.Hour + time.Duration(p.period.Minutes())*time.Minute + time.Duration(p.period.Seconds())*time.Second)
}

func getMaxDaysOfMonth(year int, month time.Month) int {
	if month == time.February && isLeapYear(year) {
		return 29
	}
	return monthMap[month]
}

func (p *Period) addDatesToTime(date time.Time, years, months, days int) time.Time {
	return p.addDatesToTimeInternal(date, years, months, days, false)
}

// addDatesToTimeMonth31Overflow behaves like addDatesToTime for all days except the 31st.
// When the input day is 31 and the target month has fewer than 31 days, instead of clamping
// to the last day of the target month the date overflows to the first of the following month.
// This makes the inclusive end equal to the last day of the target month.
// Example: Oct 31 + P1M → Dec 1 (exclusive), inclusive end = Nov 30.
// The February leap-year pre-clamps are skipped for day-31 inputs so the final overflow always fires.
func (p *Period) addDatesToTimeMonth31Overflow(date time.Time, years, months, days int) time.Time {
	return p.addDatesToTimeInternal(date, years, months, days, true)
}

func (p *Period) addDatesToTimeInternal(date time.Time, years, months, days int, overflowOn31 bool) time.Time {
	if years == 0 && months == 0 && days == 0 {
		return date
	}

	year, month, day := date.Date()

	newDay := day
	newMonth := int(month)
	newYear := year + years
	// move from 29 to 29 if applicable, or move from 29 to 28
	// February never has 31 days, so if we're starting in February, day can't be 31
	if month == time.February && day > 28 && isLeapYear(year) != isLeapYear(newYear) {
		newDay = min(newDay, getMaxDaysOfMonth(newYear, time.Month(newMonth)))
	}

	newMonth = newMonth + months
	if newMonth%12 > 0 {
		newYear += int(newMonth) / 12
		newMonth = newMonth % 12
		// move from 29 to 29 if applicable, or move from 29 to 28
		// For day-31 inputs with overflowOn31, skip the pre-clamp since overflow handles it
		if time.Month(newMonth) == time.February && newDay > 28 && isLeapYear(year) != isLeapYear(newYear) && (!overflowOn31 || day != 31) {
			newDay = min(newDay, getMaxDaysOfMonth(newYear, time.Month(newMonth)))
		}
	}

	maxDayDate := getMaxDaysOfMonth(newYear, time.Month(newMonth))
	if newDay > maxDayDate {
		if overflowOn31 && day == 31 {
			newMonth++
			if newMonth > 12 {
				newMonth = 1
				newYear++
			}
			newDay = 1
		} else {
			newDay = maxDayDate
		}
	}
	updatedDate := time.Date(newYear, time.Month(newMonth), newDay, 0, 0, 0, 0, date.Location())
	updatedDate = updatedDate.AddDate(0, 0, days)

	return updatedDate
}

func (p *Period) Copy() *Period {
	if p == nil {
		return nil
	}
	newPeriod := dateperiod.New(p.period.Years(), p.period.Months(), p.period.Weeks(), p.period.Days(), p.period.Hours(), p.period.Minutes(), p.period.Seconds())
	return &Period{period: &newPeriod}
}
