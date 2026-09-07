package mcpauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strings"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// ChallengeMethodS256 is the only code challenge method North accepts.
//
// RFC 7636 also defines "plain", where the verifier is sent as the challenge.
// That offers nothing here: every client that can compute a random verifier
// can hash it, and accepting plain would mean a challenge intercepted on the
// authorize request is enough to redeem the code. It is absent from the
// metadata document as well as refused here, so a client never selects it.
const ChallengeMethodS256 = "S256"

// verifier length bounds, from RFC 7636 section 4.1.
const (
	minVerifierLen = 43
	maxVerifierLen = 128
)

// ValidateChallenge checks a code challenge as presented on an authorize
// request.
//
// Rejecting a missing challenge rather than treating PKCE as optional: this
// endpoint only ever serves public clients, which have no secret, so the
// challenge is the only thing tying the code to whoever asked for it.
func ValidateChallenge(challenge, method string) error {
	if strings.TrimSpace(challenge) == "" {
		return apperr.Wrap(apperr.ErrValidation, "code_challenge is required")
	}
	if method != ChallengeMethodS256 {
		return apperr.Wrap(apperr.ErrValidation, "code_challenge_method must be S256")
	}
	if _, err := base64.RawURLEncoding.DecodeString(challenge); err != nil {
		return apperr.Wrap(apperr.ErrValidation,
			"code_challenge must be base64url without padding")
	}
	return nil
}

// VerifyChallenge reports whether a verifier presented at the token endpoint
// matches the challenge recorded at authorize time.
//
// The comparison is constant time. The verifier is a secret for the length of
// one exchange, and a byte-by-byte comparison that returns early would let a
// caller with a captured challenge recover it a character at a time.
func VerifyChallenge(challenge, method, verifier string) error {
	if method != ChallengeMethodS256 {
		return apperr.Wrap(apperr.ErrValidation, "unsupported code_challenge_method")
	}
	if len(verifier) < minVerifierLen || len(verifier) > maxVerifierLen {
		return apperr.Wrap(apperr.ErrValidation, "code_verifier has an invalid length")
	}

	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])

	if subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) != 1 {
		return apperr.Wrap(apperr.ErrValidation, "code_verifier does not match the challenge")
	}
	return nil
}
