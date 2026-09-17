// Package item holds the breaking-news ticker's value types.
//
// A leaf, so the templates that render a headline and the service that
// produces one never import each other. See CLAUDE.md on slice layout.
package item

import (
	"time"

	"github.com/google/uuid"
)

// Item is one headline: title, source, time and a link. Never a body.
type Item struct {
	ID          uuid.UUID
	Key         string
	Title       string
	URL         string
	Source      string
	FeedURL     string
	PublishedAt time.Time
	FetchedAt   time.Time
}

// UserFeed is a feed a person follows on top of the curated list.
type UserFeed struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	URL       string
	Title     string
	CreatedAt time.Time
}
