package util

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"gitlab.com/sqills/development/vectron/golang-packages/pkg/util"
)

func TestTimeToResponse(t *testing.T) {
	loc, _ := time.LoadLocation("Africa/Ceuta")
	datetime := time.Date(2020, 10, 20, 14, 55, 30, 999, loc)
	expected := time.Date(2020, 10, 20, 12, 55, 30, 0, time.UTC)
	assert.Equal(t, &expected, RoundedUTCTime(&datetime))
}

func TestParseTimeOptional(t *testing.T) {
	parsed, err := ParseTimeOptional("2020-12-31", "2006-01-02")
	assert.NoError(t, err)
	assert.Equal(t, time.Date(2020, time.December, 31, 0, 0, 0, 0, time.UTC), *parsed)
}

func TestParseTimeOptional_Optional(t *testing.T) {
	parsed, err := ParseTimeOptional("", "2006-01-02")
	assert.NoError(t, err)
	assert.Nil(t, parsed)
}

func TestParseTimeOptional_Err(t *testing.T) {
	parsed, err := ParseTimeOptional("awef", "2006-01-02")
	assert.Error(t, err)
	assert.Nil(t, parsed)
}

func TestParseUTCTimeOrDefault_UseValue(t *testing.T) {
	tt := "2020-01-02T15:04:05Z"
	d, _ := time.Parse("2006-01-02", "2020-12-31")

	res, err := ParseUTCTimeOrDefault(tt, time.RFC3339, &d)

	assert.NoError(t, err)
	assert.Equal(t, time.Date(2020, time.January, 2, 15, 4, 5, 0, time.UTC), *res)
}

func TestParseUTCTimeOrDefault_UseDefault(t *testing.T) {
	tt := ""
	d, _ := time.Parse("2006-01-02", "2020-12-31")

	res, err := ParseUTCTimeOrDefault(tt, time.RFC3339, &d)

	assert.NoError(t, err)
	assert.Equal(t, time.Date(2020, time.December, 31, 0, 0, 0, 0, time.UTC), *res)
}

func TestParseUTCTimeOrDefault_NilDefault(t *testing.T) {
	res, err := ParseUTCTimeOrDefault("", time.RFC3339, nil)

	assert.NoError(t, err)
	assert.Nil(t, res)
}

func TestGetHourMinuteDuration(t *testing.T) {
	assert.Equal(t, time.Duration(0), GetHourMinuteDuration(clockwork.NewFakeClockAt(time.Date(1984, time.April, 4, 0, 0, 0, 0, time.UTC)).Now()))
	assert.Equal(t, time.Duration(3720000000000), GetHourMinuteDuration(time.Date(1, 1, 1, 1, 2, 9, 9, time.UTC)))

	l, _ := time.LoadLocation("Europe/Amsterdam")
	assert.Equal(t, time.Duration(3720000000000), GetHourMinuteDuration(time.Date(1, 1, 1, 1, 2, 9, 9, l)))
}

func TestSameDay(t *testing.T) {
	d := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	assert.True(t, SameDay(d, d))
	assert.True(t, SameDay(d, d.Add(time.Hour)))
	assert.False(t, SameDay(d, d.Add(-1)))
	assert.False(t, SameDay(d, d.Add(time.Hour*24)))
	assert.False(t, SameDay(
		time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC),
	))
}

func TestParseToDuration(t *testing.T) {
	type scenario struct {
		name     string
		input    string
		expected time.Duration
		err      bool
	}

	scenarios := []scenario{
		{
			name:     "happy case",
			input:    "01:00",
			expected: time.Hour,
		},
		{
			name:     "least possible amount",
			input:    "00:00",
			expected: 0,
		},
		{
			name:     "highest amount",
			input:    "23:59",
			expected: time.Minute * 1439,
		},
		{
			name:  "invalid input",
			input: "invalid",
			err:   true,
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			d, err := ParseToDuration(s.input)

			if s.err {
				assert.Error(t, err)
				assert.Equal(t, time.Duration(0), d)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, s.expected, d)
			}
		})
	}
}

func TestParseTimeEndOfDay(t *testing.T) {
	date1 := util.Ptr(time.Date(2020, 01, 01, 01, 02, 01, 30, time.UTC))
	date2 := util.Ptr(time.Date(2020, 12, 31, 18, 30, 20, 11, time.UTC))
	date3 := util.Ptr(time.Date(2020, 05, 13, 23, 58, 49, 11, time.UTC))
	date4 := util.Ptr(time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC))

	exp1 := util.Ptr(time.Date(2020, 01, 01, 23, 59, 59, 00, time.UTC))
	exp2 := util.Ptr(time.Date(2020, 12, 31, 23, 59, 59, 00, time.UTC))
	exp3 := util.Ptr(time.Date(2020, 05, 13, 23, 59, 59, 00, time.UTC))
	exp4 := util.Ptr(time.Date(2020, 1, 2, 23, 59, 59, 00, time.UTC))

	assert.Equal(t, exp1, ParseTimeEndOfDay(date1))
	assert.Equal(t, exp2, ParseTimeEndOfDay(date2))
	assert.Equal(t, exp3, ParseTimeEndOfDay(date3))
	assert.Equal(t, exp4, ParseTimeEndOfDay(date4))
	assert.Nil(t, ParseTimeEndOfDay(nil))
}

// TestParseTimeEndOfDay_PreservesUTCCalendarDate pins the invariant the reimbursement
// day-rule for derived datetimes (TPASS-2106) depends on: a date parsed with the repo's date
// format lands at UTC midnight, and ParseTimeEndOfDay must not shift that UTC calendar date.
func TestParseTimeEndOfDay_PreservesUTCCalendarDate(t *testing.T) {
	parsed, err := time.Parse(time.DateOnly, "2026-06-02")
	assert.NoError(t, err)

	got := ParseTimeEndOfDay(&parsed)

	assert.Equal(
		t,
		parsed.Format(time.DateOnly),
		got.In(time.UTC).Format(time.DateOnly),
		"the reimbursement day rule reads the ticket's day off this UTC calendar date",
	)
}

func TestTimesDifferentPtr(t *testing.T) {
	now := time.Now()
	later := now.Add(time.Hour)

	tests := []struct {
		name string
		a, b *time.Time
		want bool
	}{
		{"both nil", nil, nil, false},
		{"a nil, b not nil", nil, &now, true},
		{"a not nil, b nil", &now, nil, true},
		{"equal times", &now, &now, false},
		{"different times", &now, &later, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TimesDifferentPtr(tt.a, tt.b)
			assert.Equal(t, tt.want, got)
		})
	}
}
