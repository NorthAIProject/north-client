package util

import (
	"time"

	dateperiod "github.com/rickb777/period"
)

// DurationToISO8601 converts a time.Duration to an ISO 8601 time duration string.
// e.g. 30*time.Minute → "PT30M", 90*time.Minute → "PT1H30M"
//
// Note: period.NewOf(d).String() is NOT used here because it normalises entirely to
// seconds (e.g. "PT1800S" for 30 minutes). By decomposing into explicit H/M/S
// components via period.New, the output remains human-readable.
func DurationToISO8601(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return dateperiod.New(0, 0, 0, 0, h, m, s).String()
}

// ParseISO8601Duration parses an ISO 8601 time duration string into a time.Duration.
// Supports H, M, S components. e.g. "PT30M" → 30*time.Minute
func ParseISO8601Duration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	p, err := dateperiod.Parse(s)
	if err != nil {
		return 0, err
	}
	d, _ := p.Duration()
	return d, nil
}
