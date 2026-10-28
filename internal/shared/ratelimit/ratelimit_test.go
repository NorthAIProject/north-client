package ratelimit_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/ratelimit"
)

type store interface {
	Allow(identifier string) (bool, error)
}

func allow(t *testing.T, s store, key string) bool {
	t.Helper()
	ok, err := s.Allow(key)
	if err != nil {
		t.Fatalf("Allow(%q): %v", key, err)
	}
	return ok
}

func TestAKeySpendsItsBurstAndIsThenDenied(t *testing.T) {
	limiters := ratelimit.New(10)

	for i := range 10 {
		if !allow(t, limiters, "caller") {
			t.Fatalf("request %d denied while the burst should still cover it", i+1)
		}
	}

	if allow(t, limiters, "caller") {
		t.Error("the 11th request was allowed; a per-minute bound of 10 did not bound anything")
	}
}

// The whole reason the pre-auth stage is keyed by address and the post-auth
// stage by account: one caller exhausting its budget must not spend anyone
// else's.
func TestOneKeyExhaustedLeavesOtherKeysUntouched(t *testing.T) {
	limiters := ratelimit.New(2)

	for range 2 {
		allow(t, limiters, "noisy")
	}
	if allow(t, limiters, "noisy") {
		t.Fatal("setup failed: the noisy key was not actually exhausted")
	}

	if !allow(t, limiters, "quiet") {
		t.Error("a second key was denied because the first spent its budget")
	}
}

func TestZeroOrNegativeRateFallsBackToTheDefault(t *testing.T) {
	// A misconfigured bound must not become "deny everything", which would take
	// the endpoint down, nor "allow everything", which would remove the bound.
	limiters := ratelimit.New(0)

	if !allow(t, limiters, "caller") {
		t.Error("a zero rate denied the first request; a bad config took the endpoint offline")
	}
}
