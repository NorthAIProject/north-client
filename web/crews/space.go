package crews

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/crews/crew"
)

// Tabs of the crew space.
const (
	TabChat      = "chat"
	TabChallenge = "challenge"
)

// SpaceView is everything the crew space page draws for one member.
type SpaceView struct {
	Space crew.Space
	// Mine are the viewer's crews, for the rail; Unread marks the ones with
	// something new.
	Mine   []crew.Crew
	Unread map[uuid.UUID]bool
	// Channel is the one open in the chat tab.
	Channel crew.SpaceChannel
	Tab     string
	Page    crew.Page
	// Board is the challenge tab's content.
	Board   crew.Board
	JoinURL string
	Me      uuid.UUID
	Loc     *time.Location
}

// ChatView is what the transcript fragments need: whose eyes, which crew
// and channel, and what the viewer may do there.
type ChatView struct {
	CrewID  uuid.UUID
	Channel crew.SpaceChannel
	Me      uuid.UUID
	MyRole  crew.Role
	// Loc is the viewer's zone, for the times on messages.
	Loc *time.Location
}

func (v SpaceView) canManage() bool { return v.Space.MyRole.CanManage() }

func (v SpaceView) chat() ChatView {
	return ChatView{CrewID: v.Space.ID, Channel: v.Channel, Me: v.Me, MyRole: v.Space.MyRole, Loc: v.Loc}
}

// at is a message's time in the viewer's zone.
func (v ChatView) at(t time.Time) time.Time {
	if v.Loc == nil {
		return t
	}
	return t.In(v.Loc)
}

// ChannelListView is the channel column's list.
type ChannelListView struct {
	CrewID    uuid.UUID
	Channels  []crew.SpaceChannel
	Open      uuid.UUID
	CanManage bool
}

func (v SpaceView) channelList() ChannelListView {
	return ChannelListView{CrewID: v.Space.ID, Channels: v.Space.Channels, Open: v.Channel.ID, CanManage: v.Space.MyRole.CanManage()}
}

func crewURL(crewID uuid.UUID) string { return "/app/crews/" + crewID.String() }

func channelURL(crewID, channelID uuid.UUID) string {
	return crewURL(crewID) + "/c/" + channelID.String()
}

func messageURL(crewID, messageID uuid.UUID) string {
	return crewURL(crewID) + "/m/" + messageID.String()
}

func liveURL(crewID, channelID uuid.UUID) string {
	return crewURL(crewID) + "/live?channel=" + channelID.String()
}

// lastSeq is the cursor after the page: the newest message's seq.
func lastSeq(p crew.Page) int64 {
	if len(p.Messages) == 0 {
		return 0
	}
	return p.Messages[len(p.Messages)-1].Seq
}

func firstSeq(p crew.Page) int64 {
	if len(p.Messages) == 0 {
		return 0
	}
	return p.Messages[0].Seq
}

func seqString(n int64) string { return strconv.FormatInt(n, 10) }

// continues is whether m sits under prev without its own name and avatar:
// same author, within five minutes, and not a reply.
func continues(prev, m crew.Message) bool {
	return prev.Kind == crew.MessageKindText && m.Kind == crew.MessageKindText &&
		prev.Author != nil && m.Author != nil && prev.Author.ID == m.Author.ID &&
		m.ReplyTo == nil && m.CreatedAt.Sub(prev.CreatedAt) < 5*time.Minute
}

// initials is what an avatar without a photo shows.
func initials(name string) string {
	fields := strings.Fields(name)
	switch len(fields) {
	case 0:
		return "?"
	case 1:
		r, _ := utf8.DecodeRuneInString(fields[0])
		return strings.ToUpper(string(r))
	default:
		a, _ := utf8.DecodeRuneInString(fields[0])
		b, _ := utf8.DecodeRuneInString(fields[len(fields)-1])
		return strings.ToUpper(string(a) + string(b))
	}
}

// segment is a run of message text, or an @mention to highlight.
type segment struct {
	Text    string
	Mention bool
}

// mentionPattern matches what the server resolves as a mention; see
// internal/crews/mentions.go.
var mentionPattern = regexp.MustCompile(`(?:^|[^\w@])(@[A-Za-z0-9_]{3,20})\b`)

// segments splits a body around @mentions, so the template can highlight
// them while every piece is still escaped like any other text.
func segments(body string) []segment {
	var out []segment
	rest := 0
	for _, m := range mentionPattern.FindAllStringSubmatchIndex(body, -1) {
		start, end := m[2], m[3]
		if start > rest {
			out = append(out, segment{Text: body[rest:start]})
		}
		out = append(out, segment{Text: body[start:end], Mention: true})
		rest = end
	}
	if rest < len(body) {
		out = append(out, segment{Text: body[rest:]})
	}
	return out
}

// mine is whether the viewer wrote m.
func (v ChatView) mine(m crew.Message) bool { return m.Author != nil && m.Author.ID == v.Me }

func (v ChatView) canManage() bool { return v.MyRole.CanManage() }

// canDelete is the author, or the owner and admins for anyone's.
func (v ChatView) canDelete(m crew.Message) bool { return v.mine(m) || v.canManage() }

// excerptOf is the start of a body, for a reply preview.
func excerptOf(body string) string {
	if utf8.RuneCountInString(body) <= 80 {
		return body
	}
	return string([]rune(body)[:79]) + "…"
}

// reportReasons are the reasons the report dialog offers, in its order.
var reportReasons = []string{"spam", "harassment", "hate", "sexual", "violence", "self_harm", "other"}
