package auth

import (
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
)

// A passkey made at sign-up must be discoverable, or the email-less
// "Use a passkey" button cannot find it.
func TestRegistrationOptionsAskForADiscoverableCredential(t *testing.T) {
	var cco protocol.PublicKeyCredentialCreationOptions
	for _, opt := range registrationOptions() {
		opt(&cco)
	}
	if got := cco.AuthenticatorSelection.ResidentKey; got != protocol.ResidentKeyRequirementPreferred {
		t.Fatalf("residentKey = %q, want %q", got, protocol.ResidentKeyRequirementPreferred)
	}
	if got := cco.AuthenticatorSelection.UserVerification; got != protocol.VerificationPreferred {
		t.Fatalf("userVerification = %q, want %q", got, protocol.VerificationPreferred)
	}
}
