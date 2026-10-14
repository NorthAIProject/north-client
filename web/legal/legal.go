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
	"fmt"
	"html"
	"time"

	"github.com/a-h/templ"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// LastUpdated is shown on both documents. Bump it whenever the substance
// changes — not for a typo, and never without re-reading what the application
// actually stores.
var LastUpdated = time.Date(2026, time.August, 29, 0, 0, 0, 0, time.UTC)

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

// prose renders one paragraph of a policy from the catalogue.
//
// A paragraph is one key, inline markup included (<strong>, <em>, links),
// because a translator has to move emphasis and links along with the words
// around them: splitting a sentence at every tag forces the English word order
// on every language. The catalogue is constant Go source reviewed like code, so
// rendering it unescaped is no different from the markup that used to sit in
// the template. The arguments are not trusted the same way and are escaped.
func prose(ctx context.Context, key string, args ...any) templ.Component {
	if len(args) == 0 {
		return templ.Raw(i18n.T(ctx, key))
	}
	escaped := make([]any, len(args))
	for i, a := range args {
		escaped[i] = html.EscapeString(fmt.Sprint(a))
	}
	return templ.Raw(i18n.Tf(ctx, key, escaped...))
}
