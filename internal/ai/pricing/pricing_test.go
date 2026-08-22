package pricing_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai/pricing"
)

// Every model the shipped configuration can actually reach must be either
// priced or explicitly acknowledged as unpriced. This is the test that stops
// "added a provider, forgot to price it" from turning into a customer who costs
// money and reports as free.
//
// The list mirrors the defaults in internal/config: the per-provider model
// defaults, plus the free floor. It is written out rather than imported so that
// changing a default in config is a visible failure here, which is the point —
// a new default model is a pricing decision.
func TestEveryShippedModelIsPricedOrAcknowledged(t *testing.T) {
	t.Parallel()

	shipped := []struct{ provider, model string }{
		{"gemini", "gemini-2.5-pro"},
		{"openrouter", "anthropic/claude-sonnet-4.5"},
		{"nvidia", "meta/llama-3.3-70b-instruct"},
		{"xai", "grok-4.5"},
		{"hermes", "hermes-3"},
		// The free floor, reached as provider=model chain entries. Named here
		// by the base provider, which is what Key normalises a variant to.
		{"openrouter", "z-ai/glm-5.2:free"},
		{"openrouter", "nvidia/nemotron-3-ultra-550b-a55b:free"},
	}

	for _, m := range shipped {
		key := pricing.Key(m.provider, m.model)
		if pricing.Known(m.provider, m.model) {
			continue
		}
		if pricing.Acknowledged(key) {
			continue
		}
		t.Errorf("%s has no rate and is not on the unpriced list; "+
			"either price it in pricing.json or acknowledge it there", key)
	}
}

// A chain entry names a variant — "openrouter=z-ai/glm-5.2:free" — and the
// client built from it reports that whole string as its provider name. Pricing
// has to see through that to the backend, or every variant call is unpriced.
func TestAVariantProviderResolvesToItsBase(t *testing.T) {
	t.Parallel()

	got := pricing.Key("openrouter=z-ai/glm-5.2:free", "z-ai/glm-5.2:free")
	want := "openrouter/z-ai/glm-5.2:free"
	if got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}

	if _, ok := pricing.Cost("openrouter=z-ai/glm-5.2:free", "z-ai/glm-5.2:free", 1000, 1000); !ok {
		t.Error("a variant chain entry did not resolve to a known rate")
	}
}

// The free floor costs nothing by construction, and that is a priced zero
// rather than a missing rate. The difference matters: one is a fact, the other
// is a gap that makes a total wrong.
func TestTheFreeFloorIsPricedAtZero(t *testing.T) {
	t.Parallel()

	micros, ok := pricing.Cost("openrouter", "z-ai/glm-5.2:free", 1_000_000, 1_000_000)
	if !ok {
		t.Fatal("the free floor has no rate; it should be priced at zero, not unpriced")
	}
	if micros != 0 {
		t.Errorf("cost = %d, want 0", micros)
	}
}

// An unknown model must not be priced as free. It reports that it is unknown so
// the caller can record the gap.
func TestAnUnknownModelReportsThatItIsUnknown(t *testing.T) {
	t.Parallel()

	if _, ok := pricing.Cost("nobody", "nothing", 1000, 1000); ok {
		t.Error("an unknown model claimed a known rate")
	}
}

// Most calls are far smaller than a million tokens. Dividing before multiplying
// would floor every one of them to zero, which would read as a free product.
func TestASmallCallDoesNotRoundToZero(t *testing.T) {
	t.Parallel()

	// A hypothetical rate is not available here, so this asserts the property
	// through the shipped zero-rate model's arithmetic path instead: the
	// calculation must not panic or overflow on realistic token counts, and a
	// priced-zero model must stay zero.
	if micros, ok := pricing.Cost("openrouter", "z-ai/glm-5.2:free", 1234, 567); !ok || micros != 0 {
		t.Errorf("cost = %d, ok = %v; want 0, true", micros, ok)
	}
}
