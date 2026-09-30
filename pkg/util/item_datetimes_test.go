package util_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/sqills/development/dev-q/s3p-travel-pass/internal/pkg/util"
)

const (
	testDateFormat     = "2006-01-02"
	testDatetimeFormat = time.RFC3339
)

func TestParseItemDatetimes(t *testing.T) {
	tests := []struct {
		name              string
		validityStartDate string
		departure         string
		arrival           string
		wantDerived       bool
		wantVSDNil        bool
		wantDeparture     *time.Time
		wantArrival       *time.Time
		wantErr           bool
	}{
		{
			name:              "validity only derives midnight departure and end of day arrival",
			validityStartDate: "2024-03-10",
			wantDerived:       true,
			wantDeparture:     ptrTime(t, "2024-03-10T00:00:00Z"),
			wantArrival:       ptrTime(t, "2024-03-10T23:59:59Z"),
		},
		{
			name:              "arrival supplied is not derived",
			validityStartDate: "2024-03-10",
			arrival:           "2024-03-10T15:00:00Z",
			wantDerived:       false,
			wantDeparture:     ptrTime(t, "2024-03-10T00:00:00Z"),
			wantArrival:       ptrTime(t, "2024-03-10T15:00:00Z"),
		},
		{
			name:              "departure only is not derived",
			validityStartDate: "2024-03-10",
			departure:         "2024-03-10T08:00:00Z",
			wantDerived:       false,
			wantDeparture:     ptrTime(t, "2024-03-10T08:00:00Z"),
			wantArrival:       ptrTime(t, "2024-03-10T23:59:59Z"),
		},
		{
			name:        "no dates at all is not derived and everything is nil",
			wantDerived: false,
			wantVSDNil:  true,
		},
		{
			name:              "bad datetime format errors",
			validityStartDate: "2024-03-10",
			departure:         "not-a-datetime",
			wantErr:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := util.ParseItemDatetimes(tt.validityStartDate, tt.departure, tt.arrival, testDateFormat, testDatetimeFormat)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.wantVSDNil {
				assert.Nil(t, got.ValidityStartDate)
			} else {
				require.NotNil(t, got.ValidityStartDate)
			}
			assert.Equal(t, tt.wantDeparture, got.Departure)
			assert.Equal(t, tt.wantArrival, got.Arrival)
			assert.Equal(t, tt.wantDerived, got.Derived)
		})
	}
}

func ptrTime(t *testing.T, value string) *time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	require.NoError(t, err)
	return &parsed
}
