package messaging

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// CodeLength is how many characters a link code has.
//
// Eight, because it is typed by hand into a chat window on a phone. That is
// deliberately far weaker than a bearer token — 8 characters of this alphabet
// is 40 bits — and the strength comes from the other three properties instead:
// the code expires in CodeTTL, it can be spent once, and redemption is rate
// limited per chat. Guessing 40 bits at a few tries a minute inside fifteen
// minutes is not a threat; leaving any of those three off would make it one.
const CodeLength = 8

// CodeTTL is how long a code stays redeemable.
//
// Long enough to switch apps, find the bot and type; short enough that a code
// left on a screen is not a standing invitation.
const CodeTTL = 15 * time.Minute

// codeAlphabet excludes characters that are read wrong when retyped: 0/O and
// 1/I/L. A person mistyping a code gets "that code is not valid" and blames
// North, so the cheapest fix is to not mint ambiguous codes.
const codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// IssueCode creates a link code for a user and returns it in the clear, once.
//
// Only the hash is stored, so there is no way to read a code back; the
// recovery path for a lost one is to issue another, which invalidates the
// first.
func (s *Service) IssueCode(ctx context.Context, userID uuid.UUID, platform string) (string, error) {
	code, err := newCode()
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256([]byte(code))
	if err := s.links.InsertCode(ctx, sum[:], userID, platform, s.now().Add(CodeTTL)); err != nil {
		return "", err
	}
	return code, nil
}

// redeem binds the chat a code was sent from to the account that issued it.
//
// The rate limit is keyed on the chat rather than the code: a caller trying
// codes in bulk has one chat and many codes, so limiting by code would count
// each attempt against a different bucket and never trigger.
func (s *Service) redeem(ctx context.Context, in InboundMessage) (uuid.UUID, error) {
	// Second lock on the same door. The Telegram adapter already refuses a
	// group before anything reaches here, and this refuses one again — because
	// what is behind the door is somebody's whole account, and a group id
	// linked by accident would hand it to everybody in that group.
	//
	// Telegram gives groups and channels negative ids and people positive ones,
	// which is what makes the check possible without this package learning what
	// a supergroup is.
	if !linkableExternalID(in.ExternalID) {
		return uuid.Nil, apperr.Wrap(apperr.ErrForbidden, "only a direct message can be linked")
	}

	if !s.redeemLimit.Allow(in.Platform + ":" + in.ExternalID) {
		return uuid.Nil, apperr.Wrap(apperr.ErrForbidden, "too many attempts")
	}

	code := normaliseCode(in.Text)
	if len(code) != CodeLength {
		return uuid.Nil, apperr.ErrNotFound
	}

	sum := sha256.Sum256([]byte(code))
	userID, err := s.links.RedeemCode(ctx, sum[:], in.Platform)
	if err != nil {
		return uuid.Nil, err
	}

	if _, err := s.links.Insert(ctx, userID, in.Platform, in.ExternalID); err != nil {
		return uuid.Nil, err
	}
	return userID, nil
}

// linkableExternalID reports whether an id may be bound to an account.
//
// Empty and negative ids are refused. Negative is Telegram's marker for a group
// or channel; a platform that numbers its people differently would need its own
// rule here, and until there is a second platform that is a guess not worth
// making. The adapter that knows the platform is the primary guard — this is
// the backstop.
func linkableExternalID(externalID string) bool {
	if externalID == "" {
		return false
	}
	// Non-numeric ids are left alone: a future platform may use handles, and
	// refusing those would break it for no gain.
	n, err := strconv.ParseInt(externalID, 10, 64)
	if err != nil {
		return true
	}
	return n > 0
}

// Unlink disconnects a platform from an account.
func (s *Service) Unlink(ctx context.Context, userID uuid.UUID, platform string) (bool, error) {
	return s.links.Delete(ctx, userID, platform)
}

// Links lists the platforms an account has connected, for the settings page.
func (s *Service) Links(ctx context.Context, userID uuid.UUID) ([]Link, error) {
	return s.links.ListByUser(ctx, userID)
}

// normaliseCode forgives the ways a code arrives from a chat window: lower
// case, padded with spaces, or prefixed with the /start command a bot deep
// link sends. What it does not do is repair mistyped characters — the
// alphabet's job is to make that unnecessary.
func normaliseCode(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "/start")
	text = strings.TrimPrefix(text, "/link")
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "-", "")
	text = strings.ReplaceAll(text, " ", "")
	return strings.ToUpper(text)
}

func newCode() (string, error) {
	buf := make([]byte, CodeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", apperr.Wrap(err, "generate link code")
	}

	// Modulo bias is negligible and irrelevant here: 256 mod 31 leaves a
	// fractional bias over an alphabet already chosen for legibility, and the
	// code's security rests on expiry and single use, not on entropy.
	out := make([]byte, CodeLength)
	for i, b := range buf {
		out[i] = codeAlphabet[int(b)%len(codeAlphabet)]
	}
	return string(out), nil
}
