// Package aiattr carries who a model call is for, and what it is for.
//
// A sibling of internal/shared/toolsurface, for the same reason that one
// exists: both ends need the value and neither may import the other. The
// metering decorator lives in internal/ai, which must not learn about users or
// jobs; the callers that know the account are feature slices that internal/ai
// sits underneath. A shared context key is the smallest thing that lets a
// worker job and an HTTP handler label their spend without either of them
// depending on the metering side.
//
// It is on the context rather than a parameter because the thing being labelled
// is several layers down — a client the caller never touches directly, reached
// through a chain and a failover loop — and threading a user id through every
// provider signature would put accounting into the interface every provider has
// to implement.
package aiattr

import (
	"context"

	"github.com/google/uuid"
)

// Attribution is who to bill a model call to and what spent it.
type Attribution struct {
	// UserID is uuid.Nil when the call belongs to no account.
	UserID uuid.UUID

	// Surface names the part of the product doing the spending. The constants
	// live in internal/spend, next to the ledger that stores them; this package
	// stays free of them so it depends on nothing.
	Surface string
}

// key namespaces the value. Unexported so no other package can write here by
// accident.
type key struct{}

// With labels the calls made under ctx.
func With(ctx context.Context, a Attribution) context.Context {
	return context.WithValue(ctx, key{}, a)
}

// WithUser is the common case: an account and a surface.
func WithUser(ctx context.Context, userID uuid.UUID, surface string) context.Context {
	return With(ctx, Attribution{UserID: userID, Surface: surface})
}

// From reads the label. The zero value means nothing set one, and a caller that
// records the uncertainty is doing the right thing — an unlabelled call is a
// wiring gap, and guessing which surface it came from would hide it.
func From(ctx context.Context) Attribution {
	a, _ := ctx.Value(key{}).(Attribution)
	return a
}
