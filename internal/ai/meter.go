package ai

import (
	"context"
)

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

	c.meter.Record(ctx, c.inner.Name(), c.resolveModel(resp.Model, req), resp.Usage, c.byok)
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
		defer close(out)

		var (
			usage Usage
			model string
		)

		for chunk := range inner {
			if chunk.Usage != nil {
				usage = *chunk.Usage
			}
			if chunk.Model != "" {
				model = chunk.Model
			}

			select {
			case out <- chunk:
			case <-ctx.Done():
				// The caller has gone. Keep draining so the provider's own
				// goroutine can finish and close, then record what was spent:
				// the tokens were consumed whether or not anyone read them.
				for rest := range inner {
					if rest.Usage != nil {
						usage = *rest.Usage
					}
					if rest.Model != "" {
						model = rest.Model
					}
				}
				c.record(ctx, model, usage, req)
				return
			}
		}

		c.record(ctx, model, usage, req)
	}()

	return out, nil
}

func (c *meteredClient) record(ctx context.Context, model string, usage Usage, req Request) {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		// Nothing to attribute. A provider that reports no usage is a gap in
		// what it tells us, not a free call, but recording a zero row would
		// bury the real ones without adding a number.
		return
	}
	// The context may already be cancelled here — the caller may have gone —
	// so the recorder is handed a context that cannot be used for a database
	// call. Detaching is the recorder's business, not this layer's, but it has
	// to be told the values, which is what WithoutCancel preserves.
	c.meter.Record(context.WithoutCancel(ctx), c.inner.Name(), c.resolveModel(model, req), usage, c.byok)
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
