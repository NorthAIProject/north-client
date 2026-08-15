package ratelimit_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/ratelimit"
)

func TestAKeySpendsItsBurstAndIsThenDenied(t *testing.T) {
	limiters := ratelimit.New(10)

	for i := range 10 {
		if !limiters.Allow("caller") {
			t.Fatalf("request %d denied while the burst should still cover it", i+1)
		}
	}

	if limiters.Allow("caller") {
		t.Error("the 11th request was allowed; a per-minute bound of 10 did not bound anything")
	}
}

// The whole reason the pre-auth stage is keyed by address and the post-auth
// stage by account: one caller exhausting its budget must not spend anyone
// else's.
func TestOneKeyExhaustedLeavesOtherKeysUntouched(t *testing.T) {
	limiters := ratelimit.New(2)

	for range 2 {
		limiters.Allow("noisy")
	}
	if limiters.Allow("noisy") {
		t.Fatal("setup failed: the noisy key was not actually exhausted")
	}

	if !limiters.Allow("quiet") {
		t.Error("a second key was denied because the first spent its budget")
	}
}

func TestZeroOrNegativeRateFallsBackToTheDefault(t *testing.T) {
	// A misconfigured bound must not become "deny everything", which would take
	// the endpoint down, nor "allow everything", which would remove the bound.
	limiters := ratelimit.New(0)

	if !limiters.Allow("caller") {
		t.Error("a zero rate denied the first request; a bad config took the endpoint offline")
	}
}
