package ai

import (
	"context"
	"log/slog"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// Runner walks a chain of providers until one of them answers.
//
// It is the coach's failover loop, lifted out so the rest of North can have it
// too. Reports, summaries, workout parsing and memory extraction each used to
// take the registry default and fail outright when that provider was rate
// limited or out of credit — which is the same provider outage the coach has
// survived since chains existed.
//
// It deliberately knows nothing about users or credentials. A tier is a plain
// string, for the reason ChainSet gives, and a caller's own provider arrives as
// an already-built client in Prepend. That keeps the aicreds package, the users
// package, and the decryption of somebody's API key on the far side of this
// boundary.
type Runner struct {
	registry *Registry
	chains   ChainSet
}

func NewRunner(registry *Registry, chains ChainSet) *Runner {
	return &Runner{registry: registry, chains: chains}
}

// RunOptions describes which providers to try and in what order.
type RunOptions struct {
	// Tier selects the chain. Empty means the default chain, which is the right
	// answer for background work that runs without a user in hand.
	Tier string

	// Prepend are tried before the chain, in the order given. The coach puts a
	// user's own provider here so a personal key is preferred but not relied
	// upon.
	Prepend []Client

	// OnError is called for every provider that refuses, including the last.
	// The coach uses it to record a bad key against the user who owns it;
	// callers with nothing to report leave it nil.
	OnError func(c Client, err error)
}

// Run tries each provider in turn until attempt succeeds, and returns the
// client that managed it.
//
// A provider that refuses on its own account — no credit, overloaded, a
// credential this deployment cannot use — is worth asking someone else. A
// malformed request is not: it would fail identically everywhere, so the walk
// stops and the caller gets the real error rather than the same error five
// providers later. Failover draws that line.
func (r *Runner) Run(ctx context.Context, opts RunOptions, attempt func(Client) error) (Client, error) {
	log := middleware.FromContext(ctx)

	clients := append(append([]Client{}, opts.Prepend...), r.registry.Resolve(r.chains.For(opts.Tier))...)
	if len(clients) == 0 {
		return nil, apperr.Wrap(apperr.ErrUnavailable, "ai: no provider is configured")
	}

	var lastErr error
	for i, client := range clients {
		err := attempt(client)
		if err == nil {
			return client, nil
		}
		lastErr = err

		if opts.OnError != nil {
			opts.OnError(client, err)
		}

		if !Failover(err) || i == len(clients)-1 {
			break
		}

		log.Warn("ai provider refused; falling back to the next in the chain",
			slog.String("provider", client.Name()),
			slog.String("next", clients[i+1].Name()),
			slog.Any("error", err),
		)
	}

	return nil, lastErr
}
