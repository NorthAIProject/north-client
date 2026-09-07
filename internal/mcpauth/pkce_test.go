package mcpauth

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

// challengeFor is what a well-behaved client computes.
func challengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

const goodVerifier = "sN1kR3vLp8qWzYx2AcBdEfGhIjKlMnOpQrStUvWxYz0"

func TestVerifyChallengeAcceptsTheMatchingVerifier(t *testing.T) {
	if len(goodVerifier) < minVerifierLen {
		t.Fatalf("the test verifier is %d characters, under the RFC minimum of %d",
			len(goodVerifier), minVerifierLen)
	}

	if err := VerifyChallenge(challengeFor(goodVerifier), ChallengeMethodS256, goodVerifier); err != nil {
		t.Errorf("the matching verifier was rejected: %v", err)
	}
}

func TestVerifyChallengeRejectsAWrongVerifier(t *testing.T) {
	challenge := challengeFor(goodVerifier)

	wrong := map[string]string{
		"a different verifier": "DIFFERENTaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"the challenge itself": challenge,
		"one character off":    goodVerifier[:len(goodVerifier)-1] + "1",
	}
	for name, verifier := range wrong {
		t.Run(name, func(t *testing.T) {
			if err := VerifyChallenge(challenge, ChallengeMethodS256, verifier); err == nil {
				t.Error("a wrong verifier was accepted")
			}
		})
	}
}

// plain is a method RFC 7636 defines and North does not accept. If it were
// accepted, a challenge captured from the authorize request would itself be
// enough to redeem the code.
func TestPlainIsRefusedEverywhere(t *testing.T) {
	if err := ValidateChallenge(goodVerifier, "plain"); err == nil {
		t.Error("ValidateChallenge accepted the plain method")
	}
	if err := ValidateChallenge(goodVerifier, ""); err == nil {
		t.Error("ValidateChallenge accepted a missing method")
	}
	if err := VerifyChallenge(goodVerifier, "plain", goodVerifier); err == nil {
		t.Error("VerifyChallenge accepted the plain method, where challenge == verifier")
	}
}

// A verifier outside the RFC's length bounds is refused before it is hashed.
// A short one is guessable, and an unbounded one is something to hash for
// free.
func TestVerifyChallengeEnforcesTheLengthBounds(t *testing.T) {
	for name, verifier := range map[string]string{
		"empty":     "",
		"too short": strings.Repeat("a", minVerifierLen-1),
		"too long":  strings.Repeat("a", maxVerifierLen+1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := VerifyChallenge(challengeFor(verifier), ChallengeMethodS256, verifier); err == nil {
				t.Error("a verifier of an invalid length was accepted")
			}
		})
	}

	// And the bounds themselves are inclusive.
	for name, verifier := range map[string]string{
		"the minimum": strings.Repeat("a", minVerifierLen),
		"the maximum": strings.Repeat("a", maxVerifierLen),
	} {
		t.Run(name, func(t *testing.T) {
			if err := VerifyChallenge(challengeFor(verifier), ChallengeMethodS256, verifier); err != nil {
				t.Errorf("a verifier at %s length was rejected: %v", name, err)
			}
		})
	}
}

func TestValidateChallengeRequiresBase64URL(t *testing.T) {
	valid := challengeFor(goodVerifier)
	if err := ValidateChallenge(valid, ChallengeMethodS256); err != nil {
		t.Errorf("a well-formed challenge was rejected: %v", err)
	}

	for name, challenge := range map[string]string{
		"empty":           "",
		"whitespace":      "   ",
		"padded":          valid + "=",
		"standard base64": strings.ReplaceAll(strings.ReplaceAll(valid, "-", "+"), "_", "/") + "+/",
		"not base64":      "not base64 at all!!",
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateChallenge(challenge, ChallengeMethodS256); err == nil {
				t.Errorf("challenge %q was accepted", challenge)
			}
		})
	}
}
