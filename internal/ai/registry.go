package ai

import (
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
