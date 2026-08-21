package ai_test

import (
	"context"
	"errors"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// stub is a named client that does nothing. The runner never calls a client
// itself — it calls the attempt function — so the methods only have to exist.
type stub struct{ name string }

func (s stub) Name() string { return s.name }

func (s stub) Chat(context.Context, ai.Request) (<-chan ai.StreamChunk, error) {
	return nil, errors.New("not used")
}

func (s stub) Generate(context.Context, ai.Request) (*ai.Response, error) {
	return nil, errors.New("not used")
}

func (s stub) UploadFile(context.Context, ai.UploadRequest) (*ai.File, error) {
	return nil, errors.New("not used")
}

// runnerWith builds a runner over the named providers, all on the default chain.
func runnerWith(names ...string) *ai.Runner {
	r := ai.NewRegistry()
	for _, name := range names {
		r.Register(stub{name: name})
	}
	return ai.NewRunner(r, ai.NewChainSet(names, nil))
}

// tried records the order the runner walked, which is the whole behaviour under
// test.
func tried(seen *[]string, fail func(name string) error) func(ai.Client) error {
	return func(c ai.Client) error {
		*seen = append(*seen, c.Name())
		return fail(c.Name())
	}
}

func TestRunStopsAtTheFirstProviderThatAnswers(t *testing.T) {
	var seen []string
	client, err := runnerWith("alpha", "beta").
		Run(context.Background(), ai.RunOptions{}, tried(&seen, func(string) error { return nil }))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if client.Name() != "alpha" {
		t.Errorf("answered by %q, want alpha", client.Name())
	}
	if len(seen) != 1 {
		t.Errorf("walked %v, want only alpha", seen)
	}
}

func TestRunWalksOnWhenAProviderRefusesOnItsOwnAccount(t *testing.T) {
	var seen []string

	// Out of credit is the case this whole change exists for.
	client, err := runnerWith("alpha", "beta", "gamma").
		Run(context.Background(), ai.RunOptions{}, tried(&seen, func(name string) error {
			if name == "alpha" {
				return apperr.Wrap(apperr.ErrPaymentRequired, "no credit")
			}
			return nil
		}))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if client.Name() != "beta" {
		t.Errorf("answered by %q, want beta", client.Name())
	}
	if len(seen) != 2 {
		t.Errorf("walked %v, want alpha then beta", seen)
	}
}

func TestRunStopsOnAnErrorEveryProviderWouldRepeat(t *testing.T) {
	// A malformed request fails identically everywhere. Walking the chain would
	// only multiply the latency before the same error reached the user.
	var seen []string
	_, err := runnerWith("alpha", "beta").
		Run(context.Background(), ai.RunOptions{}, tried(&seen, func(string) error {
			return apperr.Wrap(apperr.ErrValidation, "bad schema")
		}))
	if err == nil {
		t.Fatal("expected the error to surface")
	}
	if len(seen) != 1 {
		t.Errorf("walked %v, want to stop after alpha", seen)
	}
}

func TestRunReturnsTheLastErrorWhenEveryProviderRefuses(t *testing.T) {
	var seen []string
	_, err := runnerWith("alpha", "beta").
		Run(context.Background(), ai.RunOptions{}, tried(&seen, func(name string) error {
			return apperr.Wrap(apperr.ErrUnavailable, "%s is overloaded", name)
		}))
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(seen) != 2 {
		t.Fatalf("walked %v, want both", seen)
	}
	if !apperr.Is(err, apperr.ErrUnavailable) {
		t.Errorf("error lost its sentinel: %v", err)
	}
}

func TestRunTriesPrependedProvidersFirst(t *testing.T) {
	// This is how a user's own key is preferred without being relied upon: it
	// goes in front of the chain rather than replacing it.
	var seen []string
	client, err := runnerWith("alpha").Run(
		context.Background(),
		ai.RunOptions{Prepend: []ai.Client{stub{name: "own"}}},
		tried(&seen, func(name string) error {
			if name == "own" {
				return apperr.Wrap(apperr.ErrForbidden, "revoked key")
			}
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if client.Name() != "alpha" {
		t.Errorf("answered by %q, want the chain to catch it", client.Name())
	}
	if len(seen) != 2 || seen[0] != "own" {
		t.Errorf("walked %v, want own then alpha", seen)
	}
}

func TestRunReportsEveryRefusalThroughOnError(t *testing.T) {
	var reported []string
	var seen []string

	_, _ = runnerWith("alpha", "beta").Run(
		context.Background(),
		ai.RunOptions{OnError: func(c ai.Client, _ error) {
			reported = append(reported, c.Name())
		}},
		tried(&seen, func(name string) error {
			return apperr.Wrap(apperr.ErrUnavailable, "%s is overloaded", name)
		}),
	)

	// Including the last one: the coach records a bad key whether or not
	// anything downstream managed to answer.
	if len(reported) != 2 {
		t.Errorf("reported %v, want both refusals", reported)
	}
}

func TestRunUsesTheTierChain(t *testing.T) {
	r := ai.NewRegistry()
	r.Register(stub{name: "paid"})
	r.Register(stub{name: "free"})

	runner := ai.NewRunner(r, ai.NewChainSet(
		[]string{"paid"},
		map[string][]string{"free": {"free"}},
	))

	var seen []string
	client, err := runner.Run(context.Background(), ai.RunOptions{Tier: "free"},
		tried(&seen, func(string) error { return nil }))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if client.Name() != "free" {
		t.Errorf("answered by %q, want the free tier's own chain", client.Name())
	}
}

func TestRunFailsWhenNothingIsConfigured(t *testing.T) {
	runner := ai.NewRunner(ai.NewRegistry(), ai.NewChainSet(nil, nil))

	_, err := runner.Run(context.Background(), ai.RunOptions{}, func(ai.Client) error {
		t.Fatal("attempt must not be called with no providers")
		return nil
	})
	if !apperr.Is(err, apperr.ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
}
