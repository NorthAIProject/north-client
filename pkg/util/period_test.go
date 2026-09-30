package util

import (
	"testing"
	"time"

	"github.com/rickb777/date/v2"
	"github.com/rickb777/period"
	"github.com/stretchr/testify/assert"
	"gitlab.com/sqills/development/vectron/golang-packages/pkg/util"
)

func TestParseStringToPeriod(t *testing.T) {
	p, err := NewPeriod("P1Y6M31DT6H30M15S")
	exp := period.New(1, 6, 0, 31, 6, 30, 15)

	assert.NoError(t, err)
	assert.Equal(t, exp.String(), p.String())
}

func TestParseStringToPeriod_Nil(t *testing.T) {
	p, err := NewPeriod("")

	assert.NoError(t, err)
	assert.Nil(t, p)
}

func TestParseStringToPeriod_WeirdNegative(t *testing.T) {
	// Currently, only negative periods that start with "-" are supported. Should Go decide to support other common
	// patterns to express a negative period, this test fails. We should update the frontend validation in this case.
	p, err := NewPeriod("P-1Y")
	assert.NoError(t, err)
	assert.Equal(t, "-P1Y", p.String())
	p, err = NewPeriod("P1Y-12M-1D")
	assert.NoError(t, err)
	assert.Equal(t, "P1Y-12M-1D", p.String())
}

func TestNewPeriodZero(t *testing.T) {
	assert.Equal(t, &Period{period: util.Ptr(period.New(0, 0, 0, 0, 0, 0, 0))}, NewPeriodZero())
}

func TestParsePeriodToString(t *testing.T) {
	p := period.New(1, 6, 0, 31, 6, 30, 15)
	assert.Equal(t, "P1Y6M31DT6H30M15S", (&Period{period: &p}).String())
}

func TestParsePeriodToString_Nil(t *testing.T) {
	p := (*period.Period)(nil)
	assert.Equal(t, "", (&Period{period: p}).String())
}

func TestCopyPeriod(t *testing.T) {
	p := period.New(1, 6, 0, 31, 6, 30, 15)
	pd := &Period{period: &p}
	assert.Equal(t, p.String(), pd.Copy().String())
	assert.NotSame(t, pd, pd.Copy())
}

func TestHandlingPeriod_Only_YMD(t *testing.T) {
	ti, _ := time.Parse(time.RFC3339, "2020-05-05T22:00:00Z")
	di := date.New(2020, 5, 5)
	pYear, _ := NewPeriod("P1Y")
	pMonth, _ := NewPeriod("P1M")
	pDay, _ := NewPeriod("P1D")
	pCombi, _ := NewPeriod("P0Y2M3D")

	assert.Equal(t, "2021-05-05T00:00:00Z", pYear.AddToTimeEndExclusive(ti).Format(time.RFC3339))
	assert.Equal(t, "2021-05-05T00:00:00Z", pYear.AddToDateEndInclusiveToTime(time.UTC, di, false).Format(time.RFC3339))
	assert.Equal(t, "2020-06-05T00:00:00Z", pMonth.AddToTimeEndExclusive(ti).Format(time.RFC3339))
	assert.Equal(t, "2020-06-05T00:00:00Z", pMonth.AddToDateEndInclusiveToTime(time.UTC, di, false).Format(time.RFC3339))
	assert.Equal(t, "2020-05-06T00:00:00Z", pDay.AddToTimeEndExclusive(ti).Format(time.RFC3339))
	assert.Equal(t, "2020-05-06T00:00:00Z", pDay.AddToDateEndInclusiveToTime(time.UTC, di, false).Format(time.RFC3339))
	assert.Equal(t, "2020-07-08T00:00:00Z", pCombi.AddToTimeEndExclusive(ti).Format(time.RFC3339))
	assert.Equal(t, "2020-07-08T00:00:00Z", pCombi.AddToDateEndInclusiveToTime(time.UTC, di, false).Format(time.RFC3339))

	// test cases lopd
	assert.Equal(t, "2020-05-07T00:00:00Z", NewPeriodNoError("P2D").AddToTimeEndExclusive(ti).Format(time.RFC3339))
	assert.Equal(t, "2020-05-12T00:00:00Z", NewPeriodNoError("P1W").AddToTimeEndExclusive(ti).Format(time.RFC3339))
	assert.Equal(t, "2020-05-19T00:00:00Z", NewPeriodNoError("P2W").AddToTimeEndExclusive(ti).Format(time.RFC3339))

	ti, _ = time.Parse(time.RFC3339, "2020-01-31T00:00:00Z")
	assert.Equal(t, "2020-03-31T00:00:00Z", NewPeriodNoError("P2M").AddToTimeEndExclusive(ti).Format(time.RFC3339))

	ti, _ = time.Parse(time.RFC3339, "2020-02-29T00:00:00Z")
	assert.Equal(t, "2020-04-29T00:00:00Z", NewPeriodNoError("P2M").AddToTimeEndExclusive(ti).Format(time.RFC3339))
	assert.Equal(t, "2020-04-29T00:00:00Z", NewPeriodNoError("P2M").AddToDateEndInclusiveToTime(time.UTC, date.New(2020, 2, 29), false).Format(time.RFC3339))

	// Oct 31 + P1M: exclusive end overflows to Dec 1 so inclusive = Nov 30.
	assert.Equal(t, "2024-12-01T00:00:00Z", NewPeriodNoError("P1M").AddToDateEndInclusiveToTime(time.UTC, date.New(2024, 10, 31), false).Format(time.RFC3339))
	// Legacy: exclusive end clamps to Nov 30, so inclusive = Nov 29.
	assert.Equal(t, "2024-11-30T00:00:00Z", NewPeriodNoError("P1M").AddToDateEndInclusiveToTime(time.UTC, date.New(2024, 10, 31), true).Format(time.RFC3339))
}

func TestHandlingPeriod_On_Date(t *testing.T) {
	ti := date.New(2020, 5, 5)
	pYear, _ := NewPeriod("P1Y")
	pMonth, _ := NewPeriod("P1M")
	pDay, _ := NewPeriod("P1D")
	pCombi, _ := NewPeriod("P0Y2M3D")

	assert.Equal(t, "2021-05-04", pYear.AddToDateEndInclusive(time.UTC, ti).Format(time.DateOnly))
	assert.Equal(t, "2020-06-04", pMonth.AddToDateEndInclusive(time.UTC, ti).Format(time.DateOnly))
	assert.Equal(t, "2020-05-05", pDay.AddToDateEndInclusive(time.UTC, ti).Format(time.DateOnly))
	assert.Equal(t, "2020-07-07", pCombi.AddToDateEndInclusive(time.UTC, ti).Format(time.DateOnly))

	// test cases lopd
	assert.Equal(t, "2020-05-06", NewPeriodNoError("P2D").AddToDateEndInclusive(time.UTC, ti).Format(time.DateOnly))
	assert.Equal(t, "2020-05-11", NewPeriodNoError("P1W").AddToDateEndInclusive(time.UTC, ti).Format(time.DateOnly))
	assert.Equal(t, "2020-05-18", NewPeriodNoError("P2W").AddToDateEndInclusive(time.UTC, ti).Format(time.DateOnly))

	ti = date.New(2020, 1, 31)
	assert.Equal(t, "2020-03-30", NewPeriodNoError("P2M").AddToDateEndInclusive(time.UTC, ti).Format(time.DateOnly))

	ti = date.New(2020, 9, 30) // last day of the month
	assert.Equal(t, "2020-10-29", NewPeriodNoError("P1M").AddToDateEndInclusive(time.UTC, ti).Format(time.DateOnly))

	ti = date.New(2020, 2, 29)
	assert.Equal(t, "2020-04-28", NewPeriodNoError("P2M").AddToDateEndInclusive(time.UTC, ti).Format(time.DateOnly))

	// Aug 31 + P6M: target is Feb 2024 (leap year, 29 days) — last day = Feb 29.
	ti = date.New(2023, 8, 31)
	assert.Equal(t, "2024-02-29", NewPeriodNoError("P6M").AddToDateEndInclusive(time.UTC, ti).Format(time.DateOnly))

	// Oct 31 + P1M: target is November (30 days) — last day = Nov 30.
	ti = date.New(2024, 10, 31)
	assert.Equal(t, "2024-11-30", NewPeriodNoError("P1M").AddToDateEndInclusive(time.UTC, ti).Format(time.DateOnly))

	ti = date.New(2024, 2, 29)
	assert.Equal(t, "2025-02-27", NewPeriodNoError("P1Y").AddToDateEndInclusive(time.UTC, ti).Format(time.DateOnly))
}

func TestHandlingPeriod_On_Date_Legacy(t *testing.T) {
	// Legacy method preserves old clamping behaviour for day-31 inputs.
	ti := date.New(2023, 8, 31)
	assert.Equal(t, "2024-02-29T00:00:00Z", NewPeriodNoError("P6M").AddToDateEndInclusiveToTime(time.UTC, ti, true).Format(time.RFC3339))

	ti = date.New(2024, 10, 31)
	assert.Equal(t, "2024-11-30T00:00:00Z", NewPeriodNoError("P1M").AddToDateEndInclusiveToTime(time.UTC, ti, true).Format(time.RFC3339))
}

func NewPeriodNoError(p string) *Period {
	ps, _ := NewPeriod(p)
	return ps
}

func TestHandlingPeriod_Time(t *testing.T) {
	ti, _ := time.Parse(time.RFC3339, "2020-05-05T00:00:00Z")
	datetime, _ := time.Parse(time.RFC3339, "2020-05-05T10:00:00Z")
	pHour, _ := NewPeriod("PT1H")
	pMinute, _ := NewPeriod("PT1M")
	pSecond, _ := NewPeriod("PT1S")
	pCombi, _ := NewPeriod("PT1H10M50S")

	assert.Equal(t, "2020-05-05T01:00:00Z", pHour.AddToTimeEndExclusive(ti).Format(time.RFC3339))
	assert.Equal(t, "2020-05-05T00:01:00Z", pMinute.AddToTimeEndExclusive(ti).Format(time.RFC3339))
	assert.Equal(t, "2020-05-05T00:00:01Z", pSecond.AddToTimeEndExclusive(ti).Format(time.RFC3339))
	assert.Equal(t, "2020-05-05T01:10:50Z", pCombi.AddToTimeEndExclusive(ti).Format(time.RFC3339))

	assert.Equal(t, "2020-05-05T11:00:00Z", pHour.AddToTimeEndExclusive(datetime).Format(time.RFC3339))
	assert.Equal(t, "2020-05-05T10:01:00Z", pMinute.AddToTimeEndExclusive(datetime).Format(time.RFC3339))
	assert.Equal(t, "2020-05-05T10:00:01Z", pSecond.AddToTimeEndExclusive(datetime).Format(time.RFC3339))
	assert.Equal(t, "2020-05-05T11:10:50Z", pCombi.AddToTimeEndExclusive(datetime).Format(time.RFC3339))
}

func TestHandlingPeriod_Combi(t *testing.T) {
	ti, _ := time.Parse(time.RFC3339, "2020-05-05T00:00:00Z")
	pDayHour, _ := NewPeriod("P1DT1H")
	pCombi, _ := NewPeriod("P1MT1H10M50S")

	assert.Equal(t, "2020-05-06T01:00:00Z", pDayHour.AddToTimeEndExclusive(ti).Format(time.RFC3339))
	assert.Equal(t, "2020-06-05T01:10:50Z", pCombi.AddToTimeEndExclusive(ti).Format(time.RFC3339))
}

func TestPeriod_IsNegative(t *testing.T) {
	p, _ := NewPeriod("-P1D")

	assert.True(t, p.IsNegative())
}
