package ai_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// named is defined in registry_test.go: a fake.Client wrapped so it can carry
// an arbitrary provider name, since fake.Client is always called "fake".

func registryWith(names ...string) *ai.Registry {
	r := ai.NewRegistry()
	for _, name := range names {
		r.Register(named(name))
	}
	return r
}

func clientNames(clients []ai.Client) []string {
	out := make([]string, 0, len(clients))
	for _, c := range clients {
		out = append(out, c.Name())
	}
	return out
}

func TestResolveKeepsChainOrder(t *testing.T) {
	t.Parallel()

	r := registryWith("gemini", "openrouter", "nvidia")

	got := clientNames(r.Resolve([]string{"openrouter", "nvidia", "gemini"}))
	want := []string{"openrouter", "nvidia", "gemini"}

	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("Resolve = %v, want %v", got, want)
	}
}

// A chain naming a provider whose key is absent in this environment is the
// normal way to share one chain between a laptop and production.
func TestResolveSkipsUnregisteredNames(t *testing.T) {
	t.Parallel()

	r := registryWith("nvidia")

	got := clientNames(r.Resolve([]string{"openrouter", "hermes", "nvidia"}))
	if fmt.Sprint(got) != fmt.Sprint([]string{"nvidia"}) {
		t.Fatalf("Resolve = %v, want [nvidia]", got)
	}
}

func TestResolveDropsDuplicatesAndBlanks(t *testing.T) {
	t.Parallel()

	r := registryWith("nvidia", "hermes")

	got := clientNames(r.Resolve([]string{"nvidia", "", "hermes", "nvidia"}))
	if fmt.Sprint(got) != fmt.Sprint([]string{"nvidia", "hermes"}) {
		t.Fatalf("Resolve = %v, want [nvidia hermes]", got)
	}
}

func TestResolveEmptyChainYieldsNothing(t *testing.T) {
	t.Parallel()

	r := registryWith("nvidia")

	if got := r.Resolve(nil); len(got) != 0 {
		t.Fatalf("Resolve(nil) = %v, want empty", clientNames(got))
	}
}

func TestFailoverClassifiesErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},

		// The provider's problem: ask someone else.
		{"payment required", apperr.Wrap(apperr.ErrPaymentRequired, "openrouter"), true},
		{"unavailable", apperr.Wrap(apperr.ErrUnavailable, "openrouter"), true},
		{"forbidden", apperr.Wrap(apperr.ErrForbidden, "openrouter"), true},

		// The request's problem: it fails identically everywhere.
		{"validation", apperr.Wrap(apperr.ErrValidation, "bad schema"), false},
		{"not found", apperr.Wrap(apperr.ErrNotFound, "no such model"), false},
		{"unclassified", fmt.Errorf("nvidia returned 418: teapot"), false},

		// The user's doing: they closed the tab or the deadline passed. These
		// are built exactly as openaicompat builds them — sentinel and cause
		// both in the chain — because without the cause preserved there is no
		// way to tell them from a provider that fell over, and a cancelled
		// request would set off a walk through every remaining provider.
		{"cancelled", fmt.Errorf("%w: nvidia: %w", apperr.ErrUnavailable, context.Canceled), false},
		{"deadline exceeded", fmt.Errorf("%w: nvidia: %w", apperr.ErrUnavailable, context.DeadlineExceeded), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ai.Failover(tt.err); got != tt.want {
				t.Fatalf("Failover(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
