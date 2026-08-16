package activity_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/users"
)

// fakeRoutes stands in for *strava.Service. A local fake rather than a mock,
// and declared here rather than imported, for the same reason the interface is:
// the strava slice imports this one.
type fakeRoutes struct {
	totals activity.RouteTotals
	err    error
}

func (f fakeRoutes) RouteTotals(context.Context, uuid.UUID, time.Time, time.Time) (activity.RouteTotals, error) {
	return f.totals, f.err
}

// seedSession records a completed session that ended daysAgo days ago at noon
// in the user's own timezone, which is the zone the window is cut in.
func seedSession(t *testing.T, svc *activity.Service, user users.User, code string, daysAgo int, kcal float64) {
	t.Helper()

	loc := user.Location()
	now := time.Now().In(loc)
	noon := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, loc).AddDate(0, 0, -daysAgo)

	_, _, err := svc.Import(context.Background(), activity.ImportInput{
		UserID:       user.ID,
		ActivityCode: code,
		Source:       activity.SourceStrava,
		ExternalID:   fmt.Sprintf("%s-%d-%d", code, daysAgo, int(kcal)),
		StartedAt:    noon.Add(-time.Hour),
		EndedAt:      noon,
		WeightKg:     80,
		Calories:     kcal,
	})
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func collect(t *testing.T, src *activity.ContextSource, user users.User) coach.Context {
	t.Helper()

	var into coach.Context
	if err := src.Collect(context.Background(), coach.ContextRequest{User: user}, &into); err != nil {
		t.Fatalf("collect: %v", err)
	}
	return into
}

func TestContextSourceContributesNothingWithoutSessions(t *testing.T) {
	svc, user := newService(t, withWeight(80))

	// Someone who has never logged anything must be left exactly as they were.
	// The renderer already has an empty-state label for this heading.
	into := collect(t, activity.NewContextSource(svc, nil), user)

	if len(into.FitnessSummary) != 0 {
		t.Fatalf("FitnessSummary = %v, want nothing", into.FitnessSummary)
	}
}

func TestContextSourceReportsTheTrainingWeek(t *testing.T) {
	svc, user := newService(t, withWeight(80))

	seedSession(t, svc, user, "running_8kmh", 3, 400)
	seedSession(t, svc, user, "running_8kmh", 5, 350)

	into := collect(t, activity.NewContextSource(svc, nil), user)

	if len(into.FitnessSummary) != 2 {
		t.Fatalf("FitnessSummary = %v, want a volume line and a recovery line", into.FitnessSummary)
	}
	if !strings.Contains(into.FitnessSummary[0], "2 sessions") {
		t.Errorf("volume line = %q, want both sessions counted", into.FitnessSummary[0])
	}
	if !strings.Contains(into.FitnessSummary[0], "750 kcal") {
		t.Errorf("volume line = %q, want the week's burn", into.FitnessSummary[0])
	}
	if !strings.Contains(into.FitnessSummary[1], "5 rest days") {
		t.Errorf("recovery line = %q, want five untrained days", into.FitnessSummary[1])
	}
}

func TestContextSourceIgnoresSessionsOlderThanTheWindow(t *testing.T) {
	svc, user := newService(t, withWeight(80))

	seedSession(t, svc, user, "running_8kmh", 2, 400)
	seedSession(t, svc, user, "cycling_moderate", 30, 900)

	into := collect(t, activity.NewContextSource(svc, nil), user)

	if !strings.Contains(into.FitnessSummary[0], "1 session,") {
		t.Fatalf("volume line = %q, want only the session inside the window", into.FitnessSummary[0])
	}
	if strings.Contains(into.FitnessSummary[0], "Cycling") {
		t.Errorf("volume line = %q, want no trace of the month-old ride", into.FitnessSummary[0])
	}
}

func TestContextSourceComparesAgainstThePriorWeek(t *testing.T) {
	svc, user := newService(t, withWeight(80))

	seedSession(t, svc, user, "running_8kmh", 2, 500)
	seedSession(t, svc, user, "running_8kmh", 9, 1000) // inside the previous window

	into := collect(t, activity.NewContextSource(svc, nil), user)

	if !strings.Contains(into.FitnessSummary[1], "down 50%") {
		t.Fatalf("recovery line = %q, want the halved load reported", into.FitnessSummary[1])
	}
}

func TestContextSourceStillReportsTodaysBurn(t *testing.T) {
	svc, user := newService(t, withWeight(80))

	seedSession(t, svc, user, "running_8kmh", 0, 420)

	into := collect(t, activity.NewContextSource(svc, nil), user)

	if len(into.FitnessSummary) != 3 {
		t.Fatalf("FitnessSummary = %v, want today's burn plus the week", into.FitnessSummary)
	}
	if !strings.Contains(into.FitnessSummary[0], "burned from logged activity today") {
		t.Errorf("first line = %q, want today's burn kept ahead of the week", into.FitnessSummary[0])
	}
}

func TestContextSourceAddsRouteTotalsWhenAProviderIsWired(t *testing.T) {
	svc, user := newService(t, withWeight(80))

	seedSession(t, svc, user, "running_8kmh", 2, 400)
	routes := fakeRoutes{totals: activity.RouteTotals{Activities: 2, DistanceM: 21100, ElevationM: 300}}

	into := collect(t, activity.NewContextSource(svc, routes), user)

	if len(into.FitnessSummary) != 3 {
		t.Fatalf("FitnessSummary = %v, want a route line", into.FitnessSummary)
	}
	if !strings.Contains(into.FitnessSummary[2], "21.1 km") {
		t.Errorf("route line = %q, want the recorded distance", into.FitnessSummary[2])
	}
}

func TestContextSourceKeepsTheWeekWhenTheRouteLookupFails(t *testing.T) {
	svc, user := newService(t, withWeight(80))

	seedSession(t, svc, user, "running_8kmh", 2, 400)
	routes := fakeRoutes{err: errors.New("strava is down")}

	// Losing the distance must not cost the user their whole training summary.
	// This is the one error this source swallows, and it is swallowed on
	// purpose because the builder's own fail-soft is all-or-nothing per source.
	into := collect(t, activity.NewContextSource(svc, routes), user)

	if len(into.FitnessSummary) != 2 {
		t.Fatalf("FitnessSummary = %v, want the volume and recovery lines to survive", into.FitnessSummary)
	}
	if !strings.Contains(into.FitnessSummary[0], "1 session,") {
		t.Errorf("volume line = %q, want the week intact", into.FitnessSummary[0])
	}
}

func TestContextSourceOmitsTheRouteLineForAProviderWithNothingRecorded(t *testing.T) {
	svc, user := newService(t, withWeight(80))

	seedSession(t, svc, user, "strength_training", 2, 300)
	routes := fakeRoutes{totals: activity.RouteTotals{}}

	// A connected user who only lifted this week has no routes, which is not
	// the same as a failure and should read as silence.
	into := collect(t, activity.NewContextSource(svc, routes), user)

	if len(into.FitnessSummary) != 2 {
		t.Fatalf("FitnessSummary = %v, want no route line", into.FitnessSummary)
	}
}

func TestContextSourceIsNamedForItsSlice(t *testing.T) {
	t.Parallel()

	if got := activity.NewContextSource(nil, nil).Name(); got != "activity" {
		t.Fatalf("Name() = %q, want %q", got, "activity")
	}
}
