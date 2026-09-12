package insights

import (
	"context"
	"testing"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// A hand-edited key in the address bar must answer 404, not 500 — the handler
// decides that by matching ErrNotFound, so the sentinel has to survive Wrap.
func TestUnknownMetricIsNotFound(t *testing.T) {
	// No dependencies are touched: the key is rejected before any load runs,
	// which is also why an unknown key costs no queries.
	_, err := (&Service{}).Metric(context.Background(), users.User{}, weekRange(t), "banana")

	if err == nil {
		t.Fatal("an unknown metric key did not error")
	}
	if !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("error is %v, want it to match ErrNotFound", err)
	}
}

func TestEveryMetricIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range metrics() {
		if m.Key == "" || m.Label == "" {
			t.Errorf("metric %+v is missing a key or label", m)
		}
		if seen[m.Key] {
			t.Errorf("two metrics share the key %q, so one is unreachable", m.Key)
		}
		seen[m.Key] = true

		if m.load == nil {
			t.Errorf("metric %q has no loader", m.Key)
		}
		// Every detail page carries a link back up; an empty one renders a
		// dead control.
		if m.Href == "" {
			t.Errorf("metric %q has no domain to link back to", m.Key)
		}
	}
}
