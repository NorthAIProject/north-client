// Package crew holds the crew types, apart from internal/crews so templates
// can name them without importing the service.
package crew

import (
	"time"

	"github.com/google/uuid"
)

// Challenge kinds: so many check-ins, or so many workouts, a week.
const (
	ChallengeCheckIns = "checkins"
	ChallengeWorkouts = "workouts"
)

// Size limits, by the owner's tier: big enough for a gym crew's channels to
// feel alive, and Pro lets a club or a team use one.
// Whether larger crews are a paid feature is decided in a later phase.
const (
	MaxMembers    = 25
	MaxMembersPro = 100
)

// Role is what a person may do in a crew. The owner is crews.owner_id, the
// one place ownership lives; every other member is an admin or not.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// CanManage is true for those who run the crew: the challenge, channels,
// pins, and removing plain members.
func (r Role) CanManage() bool { return r == RoleOwner || r == RoleAdmin }

// Channel kinds: text to talk in, board for posts with threads, and wins,
// where the achievements members already share land.
const (
	ChannelText  = "text"
	ChannelBoard = "board"
	ChannelWins  = "wins"
)

type Channel struct {
	ID        uuid.UUID
	Kind      string
	Name      string
	Topic     string
	Position  int
	IsDefault bool
}

type Crew struct {
	ID      uuid.UUID
	Name    string
	OwnerID uuid.UUID
	Code    string
	Members int
}

type Challenge struct {
	Kind   string
	Target int
}

// Member is one row of the crew board: today, the streak, and this week.
type Member struct {
	ID           uuid.UUID
	DisplayName  string
	Handle       string
	CheckedIn    bool
	WorkedOut    bool
	Streak       int
	WeekProgress int
	Owner        bool
	Me           bool
	Role         Role
}

// Board is a crew as a member sees it.
type Board struct {
	Crew
	Challenge *Challenge
	Members   []Member
	// WeekStart is the Monday the challenge counts from, in the viewer's zone.
	WeekStart time.Time
	IsOwner   bool
	MyRole    Role
}

// Message kinds. A system message has no author and its body is an i18n key.
const (
	MessageKindText   = "text"
	MessageKindSystem = "system"
)

// Chat limits.
const (
	MaxBody        = 4000 // runes
	PageSize       = 50
	MaxPage        = 100
	SendsPerMinute = 20
	MaxChannels    = 20
	MaxPins        = 50
)

// Emoji is one reaction people can leave: a stable key, stored, and the
// glyph shown.
type Emoji struct {
	Key   string
	Glyph string
}

// ReactionEmoji is the gym set, in the order they are shown. The keys match
// the CHECK on crew_message_reactions.emoji.
var ReactionEmoji = []Emoji{
	{"fire", "🔥"},
	{"muscle", "💪"},
	{"clap", "👏"},
	{"heart", "❤️"},
	{"laugh", "😂"},
	{"hundred", "💯"},
	{"trophy", "🏆"},
	{"eyes", "👀"},
}

// Glyph is the emoji for key, or "" when key is not in the set.
func Glyph(key string) string {
	for _, e := range ReactionEmoji {
		if e.Key == key {
			return e.Glyph
		}
	}
	return ""
}

// Person is a crewmate as a message shows them.
type Person struct {
	ID     uuid.UUID
	Name   string
	Handle string
}

// Quote is the message a reply answers. Unavailable when the viewer may not
// see it any more: deleted, hidden, or from someone blocked either way.
type Quote struct {
	ID          uuid.UUID
	AuthorName  string
	Excerpt     string
	Unavailable bool
}

type Reaction struct {
	Emoji string
	Count int
	Mine  bool
}

// Message is one chat message as one viewer sees it.
type Message struct {
	ID         uuid.UUID
	Seq        int64
	ChannelID  uuid.UUID
	Author     *Person // nil for a system message
	Kind       string
	Body       string
	ReplyTo    *Quote
	Mentions   []uuid.UUID
	MentionsMe bool
	Reactions  []Reaction
	Pinned     bool
	Edited     bool
	Deleted    bool
	CreatedAt  time.Time
}

// Page is a run of messages in ascending order. HasMore says older messages
// exist before the first one.
type Page struct {
	Messages []Message
	HasMore  bool
}

// Unread is how much of one channel a member has not read, and how much of
// that is for them: mentions, @everyone, replies to what they wrote.
type Unread struct {
	CrewID    uuid.UUID
	ChannelID uuid.UUID
	Count     int
	Mentions  int
}

// Notify settings: which messages in a crew push a member.
const (
	NotifyAll      = "all"
	NotifyMentions = "mentions"
	NotifyNone     = "none"
)

// SpaceChannel is a channel in the list beside the chat, with what the
// viewer has not read in it.
type SpaceChannel struct {
	Channel
	Unread   int
	Mentions int
}

// SpaceMember is someone in the crew, as the member list shows them.
type SpaceMember struct {
	Person
	Role Role
}

// Space is a crew as its chat shows it to one member: the channels, the
// people, and the member's own role and notification settings.
type Space struct {
	Crew
	MyRole     Role
	Notify     string
	MutedUntil *time.Time
	Channels   []SpaceChannel
	Members    []SpaceMember
}
