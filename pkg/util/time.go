package util

import (
	"time"

	"gitlab.com/sqills/development/dev-q/s3p-travel-pass/internal/pkg/errors"
	"gitlab.com/sqills/development/vectron/golang-packages/pkg/util"
)

func TimeZeroPtr(t *time.Time) *time.Time {
	switch {
	case t == nil:
		return nil
	case t.IsZero():
		return nil
	default:
		return t
	}
}

func RoundedUTCTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	tt := t.Round(time.Second).UTC()
	return &tt
}

func ParseTimeOptional(t, layout string) (*time.Time, error) {
	if t == "" {
		return nil, nil
	}
	parsed, err := time.Parse(layout, t)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func ParseUTCTimeOrDefault(t, layout string, defaultTime *time.Time) (*time.Time, error) {
	parsed, err := ParseTimeOptional(t, layout)
	if err != nil {
		return nil, err
	}
	if parsed != nil {
		return Ptr(parsed.UTC()), nil
	}

	if defaultTime == nil {
		return nil, nil
	}

	return Ptr(defaultTime.UTC()), nil
}

func ParseTimeEndOfDay(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	return util.Ptr(t.Truncate(24 * time.Hour).Add(24*time.Hour - time.Second))
}

func GetHourMinuteDuration(tt time.Time) time.Duration {
	return time.Duration(tt.Hour())*time.Hour + time.Duration(tt.Minute())*time.Minute
}

func ParseTimeToStringOptional(t *time.Time, layout string) string {
	if t == nil {
		return ""
	}
	return t.Format(layout)
}

func ParseToDuration(v string) (time.Duration, error) {
	tt, err := time.Parse("15:04", v)
	if err != nil {
		return 0, errors.NewInternalErrorWithCause("failed to parse duration", err)
	}

	return GetHourMinuteDuration(tt), nil
}

func SameDay(v time.Time, other time.Time) bool {
	return v.YearDay() == other.YearDay() && v.Year() == other.Year()
}

func TimesDifferentPtr(a, b *time.Time) bool {
	if a == nil && b == nil {
		return false
	}
	if a == nil || b == nil {
		return true
	}
	return !a.Equal(*b)
}
