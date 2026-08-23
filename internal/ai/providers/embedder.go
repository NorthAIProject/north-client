package providers

import (
	"fmt"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/openaicompat"
)

// EmbedderOptions name the provider and model semantic retrieval should use.
type EmbedderOptions struct {
	Provider   string
	Model      string
	Dimensions int

	// Meter records what embedding calls consume. Passed separately from the
	// registry's meter because this path unwraps the registered client to reach
	// the embeddings endpoint, so the wrapper that meters chat is left behind.
	Meter ai.Meter
}

// Embedder returns the client that will produce vectors, or nil when
// embeddings are switched off.
//
// A configured provider that cannot embed is an error rather than a silent
// downgrade. Someone who set EMBEDDING_PROVIDER=openrouter has said what they
// want; starting anyway with full-text-only retrieval would look like it
// worked, and the difference would only show as worse answers weeks later.
func Embedder(registry *ai.Registry, opts EmbedderOptions) (ai.Embedder, error) {
	if opts.Provider == "" || opts.Model == "" {
		return nil, nil
	}

	client, err := registry.Get(opts.Provider)
	if err != nil {
		return nil, fmt.Errorf("providers: embedding provider %q: %w", opts.Provider, err)
	}

	// Unwrapped: the registry wraps clients for metering, and this assertion
	// reaches for a concrete type rather than the Client interface.
	compat, ok := ai.Unwrap(client).(*openaicompat.Client)
	if !ok {
		return nil, fmt.Errorf(
			"providers: %q does not serve an embeddings endpoint; use an OpenAI-dialect provider such as nvidia",
			opts.Provider)
	}

	return compat.WithEmbeddings(openaicompat.EmbedOptions{
		Model:      opts.Model,
		Dimensions: opts.Dimensions,
		Meter:      opts.Meter,
	}), nil
}
