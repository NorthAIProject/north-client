package messaging

import (
	"context"
	"time"
)

// Platform names. Not a Go enum and not a database CHECK: an adapter is the
// only thing that produces one, and the set grows by adding a directory.
const (
	PlatformTelegram = "telegram"
)

// InboundMessage is one message that arrived from a messaging platform.
//
// A struct rather than an interface, despite the ticket's wording. Interfaces
// exist so behaviour can vary; this carries data and has none. What actually
// varies per platform is delivery, and that is Transport below — making these
// interfaces would add a layer of indirection over four strings and hide the
// one seam that matters.
type InboundMessage struct {
	// Platform is which adapter this arrived through.
	Platform string

	// ExternalID is the platform's stable id for the conversation the message
	// came from — a Telegram chat id. It is the identity the adapter resolves
	// to a North account, so it must be the chat, not the message.
	ExternalID string

	// Text is what the person wrote, or the value behind a button they tapped.
	// Empty when they sent a photo with no caption.
	Text string

	// Attachment is set when the person sent a file. The adapter fills Bytes
	// before this package sees it, so no other package has to speak Telegram.
	Attachment *InboundFile

	// UpdateID is the platform's monotonic delivery counter, used to recognise
	// a redelivery. Zero when the platform has no such notion, which disables
	// the check rather than rejecting everything.
	UpdateID int64

	ReceivedAt time.Time
}

// InboundFile is a photo (or other file) that arrived with a message.
type InboundFile struct {
	Name     string
	MIMEType string
	Kind     string
	// FileID is the platform's handle. The adapter uses it to fetch Bytes
	// and may leave it set; this package never calls the platform with it.
	FileID string
	Bytes  []byte
}

// OutboundMessage is North's reply.
type OutboundMessage struct {
	Text string

	// Options is non-empty only when the coach is waiting on a confirmation
	// before it writes anything. A platform that can render buttons should;
	// one that cannot can ignore this entirely, because Text already contains
	// the question in words.
	Options []Option

	// Silent marks a reply that should not be sent at all — a redelivery that
	// was already answered. Distinct from an empty Text so a genuinely empty
	// reply is still a bug rather than a silent no-op.
	Silent bool
}

// Option is one answer a person can tap instead of typing.
type Option struct {
	// Label is what the person reads.
	Label string

	// Value is what comes back as InboundMessage.Text when they tap it, and is
	// one of the Answer constants below.
	Value string
}

// Answers to a pending confirmation. These travel as button values, so they
// are part of the wire format: changing one strands every button already sent.
const (
	AnswerApprove = "approve"
	AnswerDecline = "decline"
)

// Transport delivers a reply back to a platform.
//
// This is the whole platform seam. Telegram implements it; Discord and
// WhatsApp are a new package each rather than an edit here, which is what
// ARCHITECTURE.md means by "adding Telegram should mean adding a directory".
type Transport interface {
	// Platform reports which platform this transport speaks for, matching
	// InboundMessage.Platform.
	Platform() string

	// Send delivers one message to one conversation.
	Send(ctx context.Context, externalID string, msg OutboundMessage) error
}
