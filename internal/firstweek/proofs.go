package firstweek

import (
	"context"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// What proves each template's day, wired in cmd/web from the packages that
// own the logs:
//
//	sleep    a sleep entry dated that day (the night that ended that morning)
//	train    a completed activity session or a logged lift set
//	protein  a meal in the food log
//	journal  a journal entry
//	custom   a check-in
//
// The adapters below exist so this package does not import five feature
// slices for one yes-or-no question each: each slice already has a "list
// what was logged in this window" method, and any non-empty answer is proof.

// ProofFunc adapts a function to Proof.
type ProofFunc func(ctx context.Context, user users.User, day timerange.Range) (bool, error)

func (f ProofFunc) LoggedOn(ctx context.Context, user users.User, day timerange.Range) (bool, error) {
	return f(ctx, user, day)
}

// Listed is proof when list returns anything for the day. For list methods
// keyed by the user, like sleep.Service.ListBetween.
func Listed[T any](list func(ctx context.Context, user users.User, rg timerange.Range) ([]T, error)) Proof {
	return ProofFunc(func(ctx context.Context, user users.User, day timerange.Range) (bool, error) {
		items, err := list(ctx, user, day)
		return len(items) > 0, err
	})
}

// ListedByID is Listed for list methods keyed by the user id, like
// mind.Service.ListBetween.
func ListedByID[T any](list func(ctx context.Context, userID uuid.UUID, rg timerange.Range) ([]T, error)) Proof {
	return ProofFunc(func(ctx context.Context, user users.User, day timerange.Range) (bool, error) {
		items, err := list(ctx, user.ID, day)
		return len(items) > 0, err
	})
}

// Either is proof when any of its proofs is.
func Either(proofs ...Proof) Proof {
	return ProofFunc(func(ctx context.Context, user users.User, day timerange.Range) (bool, error) {
		for _, p := range proofs {
			ok, err := p.LoggedOn(ctx, user, day)
			if err != nil || ok {
				return ok, err
			}
		}
		return false, nil
	})
}
