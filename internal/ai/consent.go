package ai

import (
	"context"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/aiattr"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// ConsentChecker says whether a person allows their data to reach an AI
// provider. Implemented by internal/aiconsent; this package does not know what
// a user is.
type ConsentChecker interface {
	AllowsAI(ctx context.Context, userID uuid.UUID) (bool, error)
}

// Gated wraps a client so it refuses a call for anyone who has not consented.
//
// Applied at registration for the same reason metering is: a new path to a
// provider cannot be added without passing through it, and the promise on the
// consent screen covers background work nobody is watching. Clients built from
// a person's own key are not gated: connecting one is that person choosing the
// recipient.
func Gated(c Client, checker ConsentChecker) Client {
	if c == nil || checker == nil {
		return c
	}
	return &gatedClient{inner: c, checker: checker}
}

// checkConsent reads who the call is for from the attribution.
//
// A surface with no account is work that holds nobody's data, such as the
// shared daily post. No label at all is a wiring gap, so it refuses: guessing
// would send somebody's data without asking.
func checkConsent(ctx context.Context, checker ConsentChecker) error {
	a := aiattr.From(ctx)
	if a.UserID == uuid.Nil {
		if a.Surface != "" {
			return nil
		}
		middleware.FromContext(ctx).Error("ai call refused: nothing says whose data it carries")
		return apperr.Wrap(apperr.ErrAIConsentRequired, "ai: unattributed call")
	}
	allowed, err := checker.AllowsAI(ctx, a.UserID)
	if err != nil {
		// Unknown is not yes.
		return apperr.Wrap(err, "ai: read consent")
	}
	if !allowed {
		return apperr.Wrap(apperr.ErrAIConsentRequired, "ai: no consent")
	}
	return nil
}

type gatedClient struct {
	inner   Client
	checker ConsentChecker
}

func (c *gatedClient) Name() string     { return c.inner.Name() }
func (c *gatedClient) Unwrap() Client   { return c.inner }
func (c *gatedClient) CallsTools() bool { return CallsTools(c.inner) }

func (c *gatedClient) Chat(ctx context.Context, req Request) (<-chan StreamChunk, error) {
	if err := checkConsent(ctx, c.checker); err != nil {
		return nil, err
	}
	return c.inner.Chat(ctx, req)
}

func (c *gatedClient) Generate(ctx context.Context, req Request) (*Response, error) {
	if err := checkConsent(ctx, c.checker); err != nil {
		return nil, err
	}
	return c.inner.Generate(ctx, req)
}

func (c *gatedClient) UploadFile(ctx context.Context, req UploadRequest) (*File, error) {
	if err := checkConsent(ctx, c.checker); err != nil {
		return nil, err
	}
	return c.inner.UploadFile(ctx, req)
}

// GatedEmbedder does for embeddings what Gated does for chat: passages of a
// person's documents go to the same companies.
func GatedEmbedder(e Embedder, checker ConsentChecker) Embedder {
	if e == nil || checker == nil {
		return e
	}
	return &gatedEmbedder{Embedder: e, checker: checker}
}

type gatedEmbedder struct {
	Embedder
	checker ConsentChecker
}

func (e *gatedEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	if err := checkConsent(ctx, e.checker); err != nil {
		return nil, err
	}
	return e.Embedder.EmbedQuery(ctx, query)
}

func (e *gatedEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if err := checkConsent(ctx, e.checker); err != nil {
		return nil, err
	}
	return e.Embedder.Embed(ctx, texts)
}
