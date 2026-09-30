package util

import (
	"time"

	"gitlab.com/sqills/development/dev-q/s3p-travel-pass/internal/pkg/errors"
)

// ItemDatetimes carries an item's parsed travel datetimes and whether both were
// derived from the validity date because the request carried no times at all.
type ItemDatetimes struct {
	ValidityStartDate *time.Time
	Departure         *time.Time
	Arrival           *time.Time
	Derived           bool
}

// ParseItemDatetimes defaults an absent departure/arrival from the validity
// start date (midnight / end of day).
func ParseItemDatetimes(validityStartDate, departure, arrival, dateLayout, datetimeLayout string) (ItemDatetimes, error) {
	vsd, err := ParseTimeOptional(validityStartDate, dateLayout)
	if err != nil {
		return ItemDatetimes{}, errors.NewInternalErrorWithCause("unexpected validity start date format", err)
	}

	ddt, err := ParseUTCTimeOrDefault(departure, datetimeLayout, vsd)
	if err != nil {
		return ItemDatetimes{}, errors.NewInternalErrorWithCause("unexpected departure date time format", err)
	}

	adt, err := ParseUTCTimeOrDefault(arrival, datetimeLayout, ParseTimeEndOfDay(vsd))
	if err != nil {
		return ItemDatetimes{}, errors.NewInternalErrorWithCause("unexpected arrival date time format", err)
	}

	return ItemDatetimes{
		ValidityStartDate: vsd,
		Departure:         ddt,
		Arrival:           adt,
		Derived:           departure == "" && arrival == "" && vsd != nil,
	}, nil
}
