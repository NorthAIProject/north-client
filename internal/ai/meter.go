package ai

import (
	"context"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/lifecycle"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// recordTimeout bounds one spend write. A saturated pool loses the row and logs
// it, which is better than holding a goroutine or a caller until it clears.
const recordTimeout = 2 * time.Second

// Meter is told what a completed model call consumed.
//
// Deliberately primitive: provider, model, tokens, and whether the user's own
// key paid. This layer does not know what a user is or what a "surface" is, and
// giving it either would put accounting into the interface every provider has
// to implement. Whoever implements Meter reads the rest off the context.
//
// Record must not block and must not return an error. By the time it runs the
// model has already answered and the caller has already been served; a
// bookkeeping failure must never become the user's problem.
type Meter interface {
	Record(ctx context.Context, provider, model string, usage Usage, byok bool)
}

// Metered wraps a client so every call it answers is recorded.
//
// Applied at registration rather than at the call sites, because there are
// seven paths that reach a provider and an eighth will be added by someone who
// does not know this exists. Wrapping the client means a path cannot be added
// without being metered — the only way to spend money is to hold a client, and
// every client the registry hands out is wrapped.
//
// byok marks clients built from a user's own credential. Their spend is not our
// cost, and it is fixed per client rather than read per call because a client
// either belongs to a user or it does not.
func Metered(c Client, m Meter, byok bool) Client {
	if c == nil || m == nil {
		return c
	}
	return &meteredClient{inner: c, meter: m, byok: byok}
}

type meteredClient struct {
	inner Client
	meter Meter
	byok  bool
}

func (c *meteredClient) Name() string { return c.inner.Name() }

// Unwrap returns the client underneath.
//
// Needed because a Client is not always used through the interface: the
// embeddings path type-asserts to *openaicompat.Client to reach a method the
// Client interface does not carry. Without this the assertion fails against the
// wrapper — and fails only where a meter is configured, so it would work on a
// laptop and break the boot in production.
func (c *meteredClient) Unwrap() Client { return c.inner }

// CallsTools implements ToolCaller by asking the client underneath.
//
// Without it the wrapper would answer with the default, "yes", and the coach
// would never look for what a gateway did over MCP — but only where a meter is
// configured, which is production and no test.
func (c *meteredClient) CallsTools() bool { return CallsTools(c.inner) }

// UploadFile consumes no tokens, so there is nothing to record.
func (c *meteredClient) UploadFile(ctx context.Context, req UploadRequest) (*File, error) {
	return c.inner.UploadFile(ctx, req)
}

func (c *meteredClient) Generate(ctx context.Context, req Request) (*Response, error) {
	resp, err := c.inner.Generate(ctx, req)
	if err != nil || resp == nil {
		// A failed call spent nothing worth attributing. Providers bill for
		// refusals in some cases, but they do not report tokens for them, so
		// recording a zero would only add noise.
		return resp, err
	}

	c.write(ctx, resp.Model, resp.Usage, req)
	return resp, err
}

// Chat records once the stream ends, because that is when the usage arrives —
// providers send it in a final frame, after the text.
//
// Each call is its own record. A tool-calling turn makes several, and that is
// the point: the coach's own persistence keeps only the last round, so a ledger
// built from it would miss exactly the rounds with the largest transcripts.
func (c *meteredClient) Chat(ctx context.Context, req Request) (<-chan StreamChunk, error) {
	inner, err := c.inner.Chat(ctx, req)
	if err != nil {
		return inner, err
	}

	out := make(chan StreamChunk)

	go func() {
		// forward closes out itself, so a panic there or in the recorder after
		// it still leaves the reader with a closed channel.
		defer lifecycle.Recover(middleware.FromContext(ctx), "ai meter")

		usage, model := forward(inner, out)
		c.record(ctx, model, usage, req)
	}()

	return out, nil
}

// forward passes a stream on and closes out before it returns, so the caller
// is never kept waiting on the spend row that follows.
//
// It does not select on the context. The chunks that matter most, the usage and
// the error ending a reply that timed out, arrive after the context is done, and
// a reader drains to close, so nothing here can block for ever. The adapter
// underneath stops producing when the context ends.
func forward(inner <-chan StreamChunk, out chan<- StreamChunk) (usage Usage, model string) {
	defer close(out)

	for chunk := range inner {
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		out <- chunk
	}

	return usage, model
}

func (c *meteredClient) record(ctx context.Context, model string, usage Usage, req Request) {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		// Nothing to attribute. A provider that reports no usage is a gap in
		// what it tells us, not a free call, but recording a zero row would
		// bury the real ones without adding a number.
		return
	}
	c.write(ctx, model, usage, req)
}

// write hands one call's usage to the meter.
//
// The caller may already have gone, so its cancellation is stripped: the tokens
// were spent either way. Its values stay, because that is who the row belongs
// to. The deadline is what WithoutCancel takes away and Record must not block
// without one.
func (c *meteredClient) write(ctx context.Context, model string, usage Usage, req Request) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()

	c.meter.Record(ctx, c.inner.Name(), c.resolveModel(model, req), usage, c.byok)
}

// resolveModel prefers what the provider said it used, falling back to what was
// asked for. Empty stays empty: an unknown model must be visible as unknown,
// because a price is keyed on it and a guess would be priced confidently wrong.
func (c *meteredClient) resolveModel(reported string, req Request) string {
	if reported != "" {
		return reported
	}
	return req.Model
}

// Unwrap returns the client underneath any metering wrappers, or c itself.
//
// For the callers that need a concrete type rather than the interface: the
// embeddings path asserts to *openaicompat.Client to reach a method the Client
// interface does not carry. Prefer the interface; reach for this only where a
// capability is not on it.
func Unwrap(c Client) Client {
	for {
		u, ok := c.(interface{ Unwrap() Client })
		if !ok {
			return c
		}
		c = u.Unwrap()
	}
}
