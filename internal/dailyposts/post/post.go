// Package post holds the daily post types, apart from internal/dailyposts so
// templates can name them without importing the service.
package post

import (
	"time"

	"github.com/google/uuid"
)

// Post is one day's post, as one viewer sees it.
type Post struct {
	ID uuid.UUID
	// Date is the UTC day it is for, at midnight UTC.
	Date  time.Time
	Theme string
	Title string
	Body  string
	// AIGenerated says the model wrote it. Every post today is; the field is
	// what the badge reads, so a hand-written one could say otherwise.
	AIGenerated bool
	Kudos       int
	// Kudoed is whether the viewer gave kudos.
	Kudoed bool
}
