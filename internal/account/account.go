// Package account erases an account and everything North holds for it.
//
// Its own package rather than a method on internal/users, for the same reason
// internal/export is its own: it is one of the few things in North that
// legitimately spans every slice, and putting it inside one would make that
// slice import its peers for a reason unrelated to what it does. users owns
// what an account *is*; this owns what it takes to end one.
//
// The two halves of the promise live next door to each other on purpose.
// export answers "can I take my data with me", this answers "can I actually
// leave", and neither claim means much without the other.
package account

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Event names what happened to an account. These are written to a table with no
// foreign key, so they survive the account they describe.
const (
	EventExport = "export"
	EventDelete = "delete"
)

// Storage is the part of object storage this package needs: the ability to drop
// one object by key.
//
// Declared here rather than imported from media so that the dependency is the
// one method used and not another slice's whole service — the same
// consumer-side narrowing documents.Storage does.
type Storage interface {
	Delete(ctx context.Context, key string) error
}

// Erasure reports what deleting an account actually removed.
//
// The storage counts are separate from the row count because they fail
// separately: the database delete is a transaction that either happened or did
// not, while the objects are removed one call at a time afterwards, and some of
// those calls can fail without the account coming back.
type Erasure struct {
	UserID uuid.UUID

	// StorageObjects is how many objects the account owned.
	StorageObjects int

	// StorageFailures is how many of them the bucket would not drop. Anything
	// above zero means bytes outlived the account and someone has to go and
	// look.
	StorageFailures int

	// Jobs is how much queued work was thrown away with the account.
	Jobs int64

	At time.Time
}
