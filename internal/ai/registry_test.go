package ai_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestRegistryFirstRegisteredIsDefault(t *testing.T) {
	t.Parallel()

	r := ai.NewRegistry()
	r.Register(fake.Text("hello"))

	c, err := r.Default()
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	if c.Name() != "fake" {
		t.Fatalf("default = %q", c.Name())
	}
}

func TestRegistryEmptyHasNoDefault(t *testing.T) {
	t.Parallel()

	r := ai.NewRegistry()

	// A misconfigured process must fail visibly rather than return a nil client
	// that panics on first use.
	if _, err := r.Default(); !apperr.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestRegistryUnknownProvider(t *testing.T) {
	t.Parallel()

	r := ai.NewRegistry()
	r.Register(fake.Text("hello"))

	if _, err := r.Get("gemini"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := r.SetDefault("gemini"); err == nil {
		t.Fatal("defaulting to an unregistered provider must fail")
	}
}

func TestRegistryGetEmptyNameReturnsDefault(t *testing.T) {
	t.Parallel()

	r := ai.NewRegistry()
	r.Register(fake.Text("hello"))

	c, err := r.Get("")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if c.Name() != "fake" {
		t.Fatalf("expected the default, got %q", c.Name())
	}
}

func TestRegistryNamesAreSorted(t *testing.T) {
	t.Parallel()

	r := ai.NewRegistry()
	r.Register(named("openrouter"))
	r.Register(named("fake"))
	r.Register(named("gemini"))

	got := r.Names()
	want := []string{"fake", "gemini", "openrouter"}

	if len(got) != len(want) {
		t.Fatalf("Names() = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names() = %v, want %v", got, want)
		}
	}
}

// named is a client that exists only to occupy a registry slot.
type namedClient struct {
	*fake.Client
	name string
}

func (c namedClient) Name() string { return c.name }

func named(name string) ai.Client {
	return namedClient{Client: fake.Text("hello"), name: name}
}
