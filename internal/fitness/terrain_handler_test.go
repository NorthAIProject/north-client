package fitness

import (
	"net/http/httptest"
	"testing"
	"time"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func parse(t *testing.T, query string, loc *time.Location) (time.Time, int, error) {
	t.Helper()
	r := httptest.NewRequest("GET", "/app/fitness/activities/terrain"+query, nil)
	return terrainQuery(r, loc)
}

func TestTerrainQueryDefaultsToNow(t *testing.T) {
	t.Parallel()

	before, weeks, err := parse(t, "", time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !before.IsZero() {
		t.Errorf("before = %s, want the zero time meaning 'up to now'", before)
	}
	if weeks != 0 {
		t.Errorf("weeks = %d, want 0 meaning 'the service decides'", weeks)
	}
}

// A hand-edited URL asking for a Wednesday means "the week holding that
// Wednesday". Snapping is friendlier than a 422 the reader cannot act on, and
// it keeps every page boundary on a Monday regardless of what was asked for.
func TestTerrainQuerySnapsACursorBackToItsMonday(t *testing.T) {
	t.Parallel()

	before, _, err := parse(t, "?before=2026-09-09", time.UTC) // a Wednesday
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	want := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	if !before.Equal(want) {
		t.Errorf("before = %s, want %s", before, want)
	}
}

func TestTerrainQueryReadsTheCursorInTheReadersZone(t *testing.T) {
	t.Parallel()

	auckland, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	before, _, err := parse(t, "?before=2026-09-07", auckland)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if before.Location().String() != auckland.String() {
		t.Errorf("cursor resolved in %s, want %s", before.Location(), auckland)
	}
	// Midnight Monday in Auckland is the Sunday noon before it, in UTC.
	if got := before.UTC(); got.Day() != 6 || got.Hour() != 12 {
		t.Errorf("cursor is %s UTC, want 6 September 12:00", got.Format(time.RFC3339))
	}
}

func TestTerrainQueryRejectsAnUnparseableCursor(t *testing.T) {
	t.Parallel()

	_, _, err := parse(t, "?before=last-tuesday", time.UTC)
	if err == nil {
		t.Fatal("an unparseable cursor was accepted")
	}
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("error is %v, want a validation error so the endpoint answers 422", err)
	}
}

// The terrain only ever travels backwards. A future cursor is not a window
// that happens to be empty, it is a request that cannot mean anything.
func TestTerrainQueryRejectsAFutureCursor(t *testing.T) {
	t.Parallel()

	future := time.Now().AddDate(0, 0, 30).Format(time.DateOnly)

	_, _, err := parse(t, "?before="+future, time.UTC)
	if err == nil {
		t.Fatal("a future cursor was accepted")
	}
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("error is %v, want a validation error", err)
	}
}

func TestTerrainQueryRejectsNonNumericWeeks(t *testing.T) {
	t.Parallel()

	_, _, err := parse(t, "?weeks=lots", time.UTC)
	if err == nil {
		t.Fatal("a non-numeric page size was accepted")
	}
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("error is %v, want a validation error", err)
	}
}

func TestTerrainQueryPassesWeeksThroughForTheServiceToClamp(t *testing.T) {
	t.Parallel()

	// Clamping lives in one place, in the service, so the handler and the
	// service cannot disagree about the maximum.
	_, weeks, err := parse(t, "?weeks=999", time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if weeks != 999 {
		t.Errorf("weeks = %d, want it passed through untouched", weeks)
	}
}
