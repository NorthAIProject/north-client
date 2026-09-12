package messaging

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Stats renders somebody's numbers for a window as a message.
//
// A string rather than a digest type, for the same reason this package takes a
// Coach rather than a prompt: messaging refuses to learn the shape of a score.
// What arrives is words, and words are what it knows how to send.
//
// Deliberately not a model call. The insights side builds this from the same
// deterministic view the web page renders, which is what lets a digest be read
// on demand and on a schedule without metering either.
type Stats interface {
	Digest(ctx context.Context, user users.User, rg timerange.Range) (text string, photo []byte, err error)
}
