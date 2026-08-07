package ai

import (
	"context"
	"fmt"
	"sort"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Registry holds the configured providers and names one of them the default.
//
// It is built once during startup and then only read, so it needs no locking.
// Registering after the server is serving would be a bug, not a feature.
type Registry struct {
	clients     map[string]Client
	defaultName string
}

func NewRegistry() *Registry {
	return &Registry{clients: make(map[string]Client)}
}

// Register adds a client. The first one registered becomes the default until
// SetDefault says otherwise.
func (r *Registry) Register(c Client) {
	if c == nil {
		return
	}
	r.clients[c.Name()] = c
	if r.defaultName == "" {
		r.defaultName = c.Name()
	}
}

// SetDefault names the provider used when a caller does not ask for one.
func (r *Registry) SetDefault(name string) error {
	if _, ok := r.clients[name]; !ok {
		return fmt.Errorf("ai: cannot default to unregistered provider %q; registered: %v", name, r.Names())
	}
	r.defaultName = name
	return nil
}

// Get returns a named provider.
func (r *Registry) Get(name string) (Client, error) {
	if name == "" {
		return r.Default()
	}
	c, ok := r.clients[name]
	if !ok {
		return nil, apperr.Wrap(apperr.ErrNotFound, "ai: no provider named %q (registered: %v)", name, r.Names())
	}
	return c, nil
}

// Resolve returns the named providers in the order given, skipping any that
// were never registered.
//
// Silently skipping is deliberate. A chain is a preference list, and naming a
// provider whose key is absent in this environment is the normal way to express
// "use it when it is available" — a development machine and production can then
// share one chain. Callers are responsible for treating an empty result as an
// error; Build does so at startup, where a misconfiguration is cheap to fix.
//
// Duplicates are dropped so a repeated name cannot be tried twice.
func (r *Registry) Resolve(names []string) []Client {
	out := make([]Client, 0, len(names))
	seen := make(map[string]bool, len(names))

	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		c, ok := r.clients[name]
		if !ok {
			continue
		}
		seen[name] = true
		out = append(out, c)
	}

	return out
}

// Failover reports whether err justifies asking the next provider in a chain.
//
// The three sentinels below are the provider's problem, not the request's: an
// exhausted balance, an overload or rate limit, and a credential this
// deployment cannot use. Anything else — a malformed request, a schema the
// caller got wrong — would fail identically everywhere, so retrying elsewhere
// only multiplies the latency before the same error reaches the user.
func Failover(err error) bool {
	if err == nil {
		return false
	}

	// Checked before the sentinels because a cancelled or expired context
	// surfaces as a transport failure, which the OpenAI-dialect client wraps as
	// ErrUnavailable. Without this guard, a user closing the tab would set off
	// a pointless walk through every remaining provider.
	if apperr.Is(err, context.Canceled) || apperr.Is(err, context.DeadlineExceeded) {
		return false
	}

	return apperr.Is(err, apperr.ErrPaymentRequired) ||
		apperr.Is(err, apperr.ErrUnavailable) ||
		apperr.Is(err, apperr.ErrForbidden)
}

// Default returns the default provider.
func (r *Registry) Default() (Client, error) {
	if r.defaultName == "" {
		return nil, apperr.Wrap(apperr.ErrUnavailable, "ai: no providers registered")
	}
	return r.clients[r.defaultName], nil
}

// MustDefault returns the default provider and panics if there is none.
// Safe at startup, where a missing provider means the process is misconfigured
// and should not begin serving.
func (r *Registry) MustDefault() Client {
	c, err := r.Default()
	if err != nil {
		panic(err)
	}
	return c
}

// Names lists registered providers, sorted so log output and error messages are
// stable between runs.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.clients))
	for name := range r.clients {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DefaultName reports which provider is currently the default.
func (r *Registry) DefaultName() string { return r.defaultName }
