package push

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	pushdb "github.com/NorthAIProject/north-client/internal/push/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *pushdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: pushdb.New(pool)}
}

// Upsert stores a subscription, replacing keys for an endpoint already known.
func (r *Repository) Upsert(ctx context.Context, userID uuid.UUID, in Input) (Subscription, error) {
	row, err := r.q.UpsertPushSubscription(ctx, pushdb.UpsertPushSubscriptionParams{
		UserID:    userID,
		Endpoint:  strings.TrimSpace(in.Endpoint),
		P256dh:    strings.TrimSpace(in.P256dh),
		Auth:      strings.TrimSpace(in.Auth),
		UserAgent: clip(strings.TrimSpace(in.UserAgent), maxUserAgent),
	})
	if err != nil {
		return Subscription{}, apperr.Wrap(err, "upsert push subscription")
	}
	return fromDB(row), nil
}

func (r *Repository) ListByUser(ctx context.Context, userID uuid.UUID) ([]Subscription, error) {
	rows, err := r.q.ListPushSubscriptions(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list push subscriptions")
	}
	out := make([]Subscription, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

func (r *Repository) Count(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := r.q.CountPushSubscriptions(ctx, userID)
	if err != nil {
		return 0, apperr.Wrap(err, "count push subscriptions")
	}
	return int(n), nil
}

// DeleteByEndpoint removes the person's own subscription for an endpoint.
// Scoped to the user so nobody can unsubscribe a browser that is not theirs.
func (r *Repository) DeleteByEndpoint(ctx context.Context, userID uuid.UUID, endpoint string) (bool, error) {
	n, err := r.q.DeletePushSubscriptionByEndpoint(ctx, pushdb.DeletePushSubscriptionByEndpointParams{
		UserID:   userID,
		Endpoint: strings.TrimSpace(endpoint),
	})
	if err != nil {
		return false, apperr.Wrap(err, "delete push subscription")
	}
	return n > 0, nil
}

// Delete removes a subscription the push service has declared gone.
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	if err := r.q.DeletePushSubscription(ctx, id); err != nil {
		return apperr.Wrap(err, "delete push subscription")
	}
	return nil
}

func (r *Repository) MarkUsed(ctx context.Context, id uuid.UUID) error {
	if err := r.q.MarkPushSubscriptionUsed(ctx, id); err != nil {
		return apperr.Wrap(err, "mark push subscription used")
	}
	return nil
}

func (r *Repository) MarkFailed(ctx context.Context, id uuid.UUID) error {
	if err := r.q.MarkPushSubscriptionFailed(ctx, id); err != nil {
		return apperr.Wrap(err, "mark push subscription failed")
	}
	return nil
}

func fromDB(row pushdb.PushSubscription) Subscription {
	return Subscription{
		ID:         row.ID,
		UserID:     row.UserID,
		Endpoint:   row.Endpoint,
		P256dh:     row.P256dh,
		Auth:       row.Auth,
		UserAgent:  row.UserAgent,
		CreatedAt:  row.CreatedAt,
		LastUsedAt: row.LastUsedAt,
		FailedAt:   row.FailedAt,
	}
}
