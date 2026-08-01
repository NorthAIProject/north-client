package auth

import (
	"strings"
	"testing"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestValidatePassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"empty", "", true},
		{"too short", "short", true},
		{"exactly at the minimum", "123456789012", false},
		{"a passphrase", "correct horse battery staple", false},
		// bcrypt truncates silently past 72 bytes, so a longer password would
		// have a meaningless tail. Rejecting is the honest behaviour.
		{"over the bcrypt limit", strings.Repeat("a", 73), true},
		{"at the bcrypt limit", strings.Repeat("a", 72), false},
		// Length counts runes, so a short multi-byte password must still be
		// rejected rather than passing on byte count alone.
		{"multi-byte but too few characters", strings.Repeat("é", 8), true},
		{"multi-byte and long enough", strings.Repeat("é", 12), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidatePassword(tt.password)
			if tt.wantErr && err == nil {
				t.Fatalf("expected %q to be rejected", tt.name)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected %q to be accepted, got %v", tt.name, err)
			}
			if err != nil && !apperr.Is(err, apperr.ErrValidation) {
				t.Fatalf("error should classify as ErrValidation, got %v", err)
			}
		})
	}
}

func TestHashAndVerifyPassword(t *testing.T) {
	t.Parallel()

	const password = "correct horse battery staple"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	if strings.Contains(hash, password) {
		t.Fatal("the hash must not contain the plaintext")
	}
	if !VerifyPassword(hash, password) {
		t.Fatal("the correct password should verify")
	}
	if VerifyPassword(hash, password+"x") {
		t.Fatal("a wrong password must not verify")
	}
}

func TestHashPasswordIsSalted(t *testing.T) {
	t.Parallel()

	const password = "correct horse battery staple"

	first, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	second, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	// Identical passwords must not produce identical hashes, or a stolen dump
	// reveals which accounts share a password.
	if first == second {
		t.Fatal("hashing the same password twice produced the same hash; salting is broken")
	}
}

func TestVerifyPasswordAgainstEmptyHash(t *testing.T) {
	t.Parallel()

	// The login path calls this when the account does not exist. It must fail
	// rather than treat "no stored hash" as a match.
	if VerifyPassword("", "anything at all") {
		t.Fatal("an empty stored hash must never verify")
	}
}

func TestConfirmationMatches(t *testing.T) {
	t.Parallel()

	if !ConfirmationMatches("a passphrase here", "a passphrase here") {
		t.Fatal("identical values should match")
	}
	if ConfirmationMatches("a passphrase here", "a passphrase there") {
		t.Fatal("different values must not match")
	}
}
