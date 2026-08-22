package quota_test

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/quota"
)

// A tier may raise the count. It may not change the window, because
// quota_counters is keyed on the window floor: two tiers with different windows
// put the same account on different rows, so changing tier mid-window would
// silently hand the user a fresh budget.
func TestATierCannotChangeTheWindow(t *testing.T) {
	t.Parallel()

	limits := quota.NewLimits(
		map[quota.Action]quota.Limit{quota.CoachMessage: {PerWindow: 10, Window: time.Hour}},
		map[string]map[quota.Action]quota.Limit{
			"pro": {quota.CoachMessage: {PerWindow: 100, Window: 24 * time.Hour}},
		},
	)

	pro := limits.For("pro")[quota.CoachMessage]
	if pro.PerWindow != 100 {
		t.Errorf("PerWindow = %d, want the tier's own 100", pro.PerWindow)
	}
	if pro.Window != time.Hour {
		t.Errorf("Window = %v, want the shared 1h; a per-tier window resets the counter on upgrade", pro.Window)
	}
}

// An unset window still gets the package default rather than zero, which
// Consume would read as "no budget configured" and let through unbounded.
func TestAMissingWindowTakesTheDefault(t *testing.T) {
	t.Parallel()

	limits := quota.NewLimits(map[quota.Action]quota.Limit{
		quota.CoachMessage: {PerWindow: 5},
	}, nil)

	if got := limits.For("")[quota.CoachMessage].Window; got != quota.DefaultWindow {
		t.Errorf("Window = %v, want %v", got, quota.DefaultWindow)
	}
}

// An empty tier is discarded rather than stored, so an unset environment
// variable cannot leave a tier resolving to a map with no budgets in it.
func TestAnEmptyTierIsDiscarded(t *testing.T) {
	t.Parallel()

	fallback := map[quota.Action]quota.Limit{quota.CoachMessage: {PerWindow: 7, Window: time.Hour}}
	limits := quota.NewLimits(fallback, map[string]map[quota.Action]quota.Limit{
		"pro": {},
		"":    {quota.CoachMessage: {PerWindow: 99}},
	})

	if got := limits.For("pro")[quota.CoachMessage].PerWindow; got != 7 {
		t.Errorf("PerWindow = %d, want the fallback 7 for a tier configured with nothing", got)
	}
}

// NewLimits copies, so a caller that reuses its map cannot reach in and change
// a budget after the service is running.
func TestLimitsDoNotAliasTheCallersMap(t *testing.T) {
	t.Parallel()

	fallback := map[quota.Action]quota.Limit{quota.CoachMessage: {PerWindow: 3, Window: time.Hour}}
	limits := quota.NewLimits(fallback, nil)

	fallback[quota.CoachMessage] = quota.Limit{PerWindow: 9999, Window: time.Hour}

	if got := limits.For("")[quota.CoachMessage].PerWindow; got != 3 {
		t.Errorf("PerWindow = %d, want 3; the caller's map is still aliased", got)
	}
}
