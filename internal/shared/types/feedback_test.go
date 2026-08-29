package types_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/types"
)

func TestParseHelpful(t *testing.T) {
	t.Parallel()

	yes, err := types.ParseHelpful(types.HelpfulYes)
	if err != nil || yes == nil || !*yes {
		t.Errorf("HelpfulYes parsed to %v, %v", yes, err)
	}

	no, err := types.ParseHelpful(types.HelpfulNo)
	if err != nil || no == nil || *no {
		t.Errorf("HelpfulNo parsed to %v, %v", no, err)
	}

	// nil with no error is "they took it back", which a caller must not confuse
	// with a rejected value. The error is the only signal of a bad answer.
	cleared, err := types.ParseHelpful(types.HelpfulClear)
	if err != nil {
		t.Errorf("HelpfulClear errored: %v", err)
	}
	if cleared != nil {
		t.Errorf("HelpfulClear parsed to %v, want nil", cleared)
	}
}

func TestParseHelpfulRejectsAnythingElse(t *testing.T) {
	t.Parallel()

	// Empty string included: a missing form field must be an error rather than
	// silently clearing an answer somebody gave on purpose.
	for _, value := range []string{"", "yes", "no", "up", "down", "true", "HELPFUL"} {
		if _, err := types.ParseHelpful(value); err == nil {
			t.Errorf("ParseHelpful(%q) was accepted", value)
		}
	}
}
