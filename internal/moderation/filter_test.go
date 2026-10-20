package moderation_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/moderation"
)

// Gym talk swears; the filter only stops what is on the lists, as whole words,
// however it is dressed up.
func TestFilter(t *testing.T) {
	f := moderation.NewFilter()

	for _, ok := range []string{
		"",
		"this set was fucking brutal",
		"Scunthorpe",
		"shitake risotto",
		"assassin",
		"cabrón, que serie",
		"hit 100kg today!!",
	} {
		if err := f.Check(ok); err != nil {
			t.Errorf("Check(%q) = %v; want allowed", ok, err)
		}
	}

	lists := moderation.TermsForTest()
	if len(lists) < 3 {
		t.Fatalf("%d word lists; want en, es and pt", len(lists))
	}
	for locale, terms := range lists {
		if len(terms) == 0 {
			t.Fatalf("%s list is empty", locale)
		}
		term := terms[0]
		for _, bad := range []string{
			term,
			strings.ToUpper(term),
			"you " + term + "!",
			leet(term),
			accented(term),
		} {
			if err := f.Check(bad); !errors.Is(err, moderation.ErrBlockedContent) {
				t.Errorf("%s: Check(%q) = %v; want ErrBlockedContent", locale, bad, err)
			}
		}
	}
}

// leet swaps every letter the filter knows a stand-in for.
func leet(s string) string {
	return strings.NewReplacer("a", "4", "e", "3", "i", "1", "o", "0", "s", "$").Replace(s)
}

// accented puts a combining acute accent on the first letter.
func accented(s string) string {
	r := []rune(s)
	return string(r[0]) + "́" + string(r[1:])
}
