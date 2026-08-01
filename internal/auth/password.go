package auth

import (
	"crypto/subtle"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// bcryptCost is the work factor. The default (10) is too low for 2026 hardware;
// 12 keeps a single hash in the low tens of milliseconds, which is unnoticeable
// on a login and expensive in bulk for anyone with a stolen dump.
const bcryptCost = 12

// minPasswordLength follows current NIST guidance: length is what matters, and
// composition rules mostly push people towards predictable substitutions.
const minPasswordLength = 12

// bcrypt silently truncates at 72 bytes, so anything longer would make the tail
// of a passphrase meaningless. Rejecting is honest; truncating is not.
const maxPasswordLength = 72

// HashPassword validates and hashes a plaintext password.
func HashPassword(plaintext string) (string, error) {
	if err := ValidatePassword(plaintext); err != nil {
		return "", err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcryptCost)
	if err != nil {
		return "", apperr.Wrap(err, "hash password")
	}
	return string(hash), nil
}

// ValidatePassword reports whether a password is acceptable.
func ValidatePassword(plaintext string) error {
	var errs apperr.FieldErrors

	switch {
	case plaintext == "":
		errs = errs.Add("password", "Password is required.")
	case utf8.RuneCountInString(plaintext) < minPasswordLength:
		errs = errs.Add("password", "Use at least 12 characters. A short sentence works well.")
	case len(plaintext) > maxPasswordLength:
		errs = errs.Add("password", "Password must be 72 bytes or fewer.")
	}

	return errs.OrNil()
}

// VerifyPassword reports whether plaintext matches hash.
//
// It always performs work, even when the account does not exist, because a fast
// "no such user" and a slow "wrong password" tell an attacker which addresses
// are registered.
func VerifyPassword(hash, plaintext string) bool {
	if hash == "" {
		// No stored hash: hash the input anyway so the timing matches the real
		// path, then fail.
		_, _ = bcrypt.GenerateFromPassword([]byte(plaintext), bcryptCost)
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
}

// ConfirmationMatches compares a password with its confirmation field in
// constant time.
func ConfirmationMatches(password, confirmation string) bool {
	return subtle.ConstantTimeCompare([]byte(password), []byte(confirmation)) == 1
}
