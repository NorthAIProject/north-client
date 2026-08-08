package openaicompat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// EmbedOptions configure the embedding side of a client.
//
// Separate from Options because a provider speaking the chat dialect does not
// necessarily serve /embeddings — OpenRouter and xAI do not. A client without
// these is a chat client and nothing more, and asserting ai.Embedder on it
// fails, which is the honest answer.
type EmbedOptions struct {
	// Model is the embedding model. Required to enable embeddings at all.
	Model string

	// Dimensions is how long its vectors are. Required, because the database
	// column is a fixed width and a mismatch has to be caught at startup
	// rather than on the first insert of a reindex.
	Dimensions int

	// MaxBatch bounds one request. Providers cap inputs per call; exceeding it
	// fails the whole batch, so the client splits rather than finding out.
	MaxBatch int
}

const defaultEmbedBatch = 64

// WithEmbeddings returns a copy of the client that can also embed.
func (c *Client) WithEmbeddings(opts EmbedOptions) *Client {
	clone := *c
	if opts.MaxBatch <= 0 {
		opts.MaxBatch = defaultEmbedBatch
	}
	clone.embed = opts
	return &clone
}

// EmbedModel and Dimensions report what this client's vectors are, so a stored
// vector can record its provenance and be invalidated when either changes.
func (c *Client) EmbedModel() string { return c.embed.Model }
func (c *Client) Dimensions() int    { return c.embed.Dimensions }

// CanEmbed reports whether this client was configured with an embedding model.
func (c *Client) CanEmbed() bool { return c.embed.Model != "" && c.embed.Dimensions > 0 }

// Embed returns one vector per input, in the input's order.
//
// Order matters more than it looks: the caller pairs the results with the
// chunks it sent, so a response that came back reordered would attach every
// vector to the wrong passage and quietly make retrieval nonsense. The
// provider returns an index per embedding, and this sorts by it rather than
// trusting the order they arrived in.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if !c.CanEmbed() {
		return nil, fmt.Errorf("%s: no embedding model configured", c.name)
	}
	if len(texts) == 0 {
		return nil, nil
	}

	out := make([][]float32, 0, len(texts))

	for start := 0; start < len(texts); start += c.embed.MaxBatch {
		end := min(start+c.embed.MaxBatch, len(texts))

		batch, err := c.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}

	if len(out) != len(texts) {
		return nil, fmt.Errorf("%s: asked for %d embeddings and received %d", c.name, len(texts), len(out))
	}
	return out, nil
}

func (c *Client) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body := map[string]any{
		"model": c.embed.Model,
		"input": texts,
	}

	// NVIDIA's NIM endpoints require an input_type on every request; providers
	// that do not know the field ignore it. "passage" is right for indexing,
	// and asymmetric models are the common case — see EmbedQuery.
	body["input_type"] = "passage"

	resp, err := c.postTo(ctx, "/embeddings", body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var decoded struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&decoded); err != nil {
		return nil, apperr.Wrap(err, "%s: decode embeddings", c.name)
	}

	sort.Slice(decoded.Data, func(i, j int) bool { return decoded.Data[i].Index < decoded.Data[j].Index })

	out := make([][]float32, 0, len(decoded.Data))
	for _, d := range decoded.Data {
		if len(d.Embedding) != c.embed.Dimensions {
			// A silent dimension change is the worst version of this failure:
			// the insert would fail far away, during a reindex, with an error
			// naming a column rather than a model.
			return nil, fmt.Errorf("%s: %s returned %d dimensions, expected %d — the configured dimensions do not match the model",
				c.name, c.embed.Model, len(d.Embedding), c.embed.Dimensions)
		}
		out = append(out, d.Embedding)
	}
	return out, nil
}

// EmbedQuery embeds a search query rather than a passage.
//
// Asymmetric embedding models — which most retrieval models are — are trained
// with a different prefix for the question and the thing being searched. Using
// the passage side for both costs real recall, and it is invisible: results
// still come back, just worse ones.
func (c *Client) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	if !c.CanEmbed() {
		return nil, fmt.Errorf("%s: no embedding model configured", c.name)
	}

	resp, err := c.postTo(ctx, "/embeddings", map[string]any{
		"model":      c.embed.Model,
		"input":      []string{query},
		"input_type": "query",
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var decoded struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&decoded); err != nil {
		return nil, apperr.Wrap(err, "%s: decode query embedding", c.name)
	}
	if len(decoded.Data) == 0 {
		return nil, fmt.Errorf("%s: no embedding returned for the query", c.name)
	}
	return decoded.Data[0].Embedding, nil
}

var _ ai.Embedder = (*Client)(nil)
