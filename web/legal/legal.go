// Package legal renders the privacy policy and terms.
//
// They live in the codebase rather than a CMS for one reason: a policy that
// describes something other than what the software does is worse than no
// policy, and the only way to keep the two in step is to make them change in
// the same commit. Every category below was read off the schema and the
// integration wiring, not written from a template.
package legal

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/auth"
)

// LastUpdated is shown on both documents. Bump it whenever the substance
// changes — not for a typo, and never without re-reading what the application
// actually stores.
const LastUpdated = "29 August 2026"

// The details a lawyer, not a program, has to supply.
//
// Deliberately loud rather than plausible. The same reasoning as the
// `khepri.invalid` hostname in the deployment values: a placeholder that looks
// like a real answer gets shipped, and a placeholder that obviously is not
// gets fixed. Nothing here should reach a public deployment unchanged, and the
// legal-page test fails while any of them is still in place.
const (
	OperatorName    = "OPERATOR_LEGAL_NAME_UNSET"
	OperatorAddress = "OPERATOR_ADDRESS_UNSET"
	ContactEmail    = "privacy@khepri.invalid"
	Jurisdiction    = "JURISDICTION_UNSET"
)

// Placeholders is every value that must be replaced before launch. The test
// reads this rather than a hand-kept list, so adding one above cannot leave it
// unchecked.
func Placeholders() []string {
	return []string{OperatorName, OperatorAddress, ContactEmail, Jurisdiction}
}

func signedIn(ctx context.Context) bool {
	_, ok := auth.UserFrom(ctx)
	return ok
}
