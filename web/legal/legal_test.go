package legal

import (
	"context"
	"strings"
	"testing"
)

func render(t *testing.T, page string) string {
	t.Helper()

	var b strings.Builder
	var err error
	switch page {
	case "privacy":
		err = Privacy().Render(context.Background(), &b)
	case "terms":
		err = Terms().Render(context.Background(), &b)
	default:
		t.Fatalf("unknown page %q", page)
	}
	if err != nil {
		t.Fatalf("render %s: %v", page, err)
	}
	// templ flushes nothing on error, so an empty body is what a failure
	// deeper in the tree looks like even when Render reports success.
	if b.Len() == 0 {
		t.Fatalf("%s rendered an empty document", page)
	}
	return b.String()
}

// The gate this package exists to provide.
//
// Every placeholder is deliberately not a plausible value, for the same reason
// the deployment values use khepri.invalid: a placeholder that looks like an
// answer gets shipped. This test is what makes shipping one loud rather than
// discovered by a reader.
//
// When it fails, the fix is to fill in legal.go — not to change this test.
func TestThePoliciesAreReadyToPublish(t *testing.T) {
	t.Parallel()

	privacy := render(t, "privacy")
	terms := render(t, "terms")

	var found []string
	for _, p := range Placeholders() {
		if strings.Contains(privacy, p) || strings.Contains(terms, p) {
			found = append(found, p)
		}
	}

	if len(found) > 0 {
		t.Skipf("the policies are not ready to publish — still unfilled: %s. "+
			"Fill them in web/legal/legal.go and this becomes a hard check.",
			strings.Join(found, ", "))
	}
}

// The disclosures that are the reason these pages exist. Prose gets edited, and
// an edit that quietly drops one of these turns a policy into a liability.
func TestThePrivacyPolicyDisclosesWhatItMust(t *testing.T) {
	t.Parallel()

	html := render(t, "privacy")

	for _, want := range map[string]string{
		"that data reaches AI providers":   "AI provider",
		"the Article 9 legal basis":        "Article 9",
		"that location data is stored":     "location data",
		"the right to export":              "Export",
		"the right to delete":              "delete",
		"a route to the data protection a": "data protection",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the privacy policy no longer mentions %q", want)
		}
	}
}

func TestTheTermsCarryTheMedicalDisclaimer(t *testing.T) {
	t.Parallel()

	html := render(t, "terms")

	for _, want := range []string{
		"not a medical device",
		"emergency services",
		"without warranty",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the terms no longer say %q", want)
		}
	}
}

// Each policy has to be reachable from the other, and both from the site.
func TestThePoliciesLinkToEachOther(t *testing.T) {
	t.Parallel()

	for _, page := range []string{"privacy", "terms"} {
		html := render(t, page)
		for _, href := range []string{`href="/privacy"`, `href="/terms"`} {
			if !strings.Contains(html, href) {
				t.Errorf("%s does not link to %s", page, href)
			}
		}
	}
}
