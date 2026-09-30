// Package friend holds the social types, apart from internal/social so the
// web templates can name them without importing the service that renders
// them.
package friend

import (
	"time"

	"github.com/google/uuid"
)

// Follow statuses. A follow waits for the followee's yes: growth data is
// personal, so nobody is followable without consent.
const (
	StatusPending  = "pending"
	StatusAccepted = "accepted"
)

// Invite channels, recorded so the strangers-count can say which posting of
// a link brought somebody in.
const (
	ChannelLink     = "link"
	ChannelMessages = "messages"
	ChannelX        = "x"
	ChannelFacebook = "facebook"
	ChannelContacts = "contacts"
)

// Person is another account as this package shows it: a name to display and
// the handle that links to them. Never an email, never anything logged.
type Person struct {
	ID          uuid.UUID
	DisplayName string
	// Handle is empty for somebody who never chose one.
	Handle string
}

// Connection is a person and where the follow between you stands.
type Connection struct {
	Person
	Status string
	Since  time.Time
}

// Invite is one person's link for one channel.
type Invite struct {
	Code      string
	InviterID uuid.UUID
	Channel   string
}

// InvitePreview is what a signed-out visitor sees on /i/<code>: who asked
// them, and nothing else about that person.
type InvitePreview struct {
	Code    string
	Channel string
	Inviter Person
}

// Overview is the Friends page: your handle, your link, and every connection.
type Overview struct {
	Handle string
	Invite Invite
	// Joined is how many accounts came in through your links.
	Joined int
	// Requests are people waiting for your yes.
	Requests []Connection
	// Followers have your yes. Following includes requests you sent that are
	// still pending, marked by their status.
	Followers []Connection
	Following []Connection
	Blocked   []Person
}

// Relationship is how a viewer stands to a profile they are looking at.
type Relationship struct {
	// You follow them, or asked to (pending).
	Following string
	// They follow you (accepted only).
	FollowsYou bool
	// Self is your own profile.
	Self bool
}

// Profile is /u/<handle>: public name, counts, and the viewer's standing.
type Profile struct {
	Person
	Followers    int
	Following    int
	Relationship Relationship
}
