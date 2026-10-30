package coach

import (
	"context"
	"testing"
	"unicode/utf8"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

func TestTheStatusLineIsInTheReadersLanguage(t *testing.T) {
	cases := map[string]string{
		"en":    "is checking your goals",
		"es":    "está revisando tus objetivos",
		"pt-BR": "está verificando seus objetivos",
		"pt-PT": "está a ver os teus objetivos",
	}
	for tag, want := range cases {
		ctx := i18n.WithLocale(context.Background(), tag)
		if got := toolStatus(ctx, []ai.ToolCall{{Name: "list_goals"}}); got != want {
			t.Errorf("%s: toolStatus = %q, want %q", tag, got, want)
		}
	}
}

// A capability added to the registry without a label still says something
// true, rather than nothing or its snake_case name.
func TestAnUnlabelledToolFallsBackToAGenericLine(t *testing.T) {
	got := toolStatus(context.Background(), []ai.ToolCall{{Name: "brand_new_tool"}})
	if got != "is looking things up" {
		t.Errorf("toolStatus = %q, want the default line", got)
	}
	if got := toolStatus(context.Background(), nil); got != "is looking things up" {
		t.Errorf("toolStatus(nil) = %q, want the default line", got)
	}
}

// The first call names a round; the header has room for one phrase.
func TestTheFirstCallNamesTheRound(t *testing.T) {
	got := toolStatus(context.Background(), []ai.ToolCall{{Name: "search_exercises"}, {Name: "list_goals"}})
	if got != "is looking up exercises" {
		t.Errorf("toolStatus = %q, want the first call's label", got)
	}
}

// The contract caps the verb phrase at 32 characters: the pill does not wrap,
// and on a 390px phone anything longer runs under the header buttons.
func TestToolStatusLabelsFitTheHeader(t *testing.T) {
	for _, tag := range []string{"en", "es", "pt-BR", "pt-PT"} {
		for _, key := range ToolStatusKeys() {
			label := i18n.Translate(tag, key)
			if label == key {
				t.Errorf("%s: %s has no copy", tag, key)
				continue
			}
			if n := utf8.RuneCountInString(label); n > 32 {
				t.Errorf("%s: %s = %q is %d characters, the contract allows 32", tag, key, label, n)
			}
		}
	}
}

func TestAStatusLineCannotBreakTheFrame(t *testing.T) {
	if got, want := sseLine("is\nchecking\r\n  things"), "is checking things"; got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
}
