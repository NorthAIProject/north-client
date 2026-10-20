package dailyposts

import (
	"slices"
	"strings"
	"testing"
)

const goodTitle = "A glass of water before each meal"

const goodBody = `Most people drink less than they think, and the gap shows up as an afternoon
slump more often than as thirst. Water is easy to forget when the day gets busy,
which is exactly when a small habit helps.

Today, try one simple anchor: a glass of water before each meal. It ties drinking
to something you already do three times a day, so it needs no reminder and no
app. If plain water feels dull, a slice of lemon or a splash of juice changes
nothing that matters.

Notice how the afternoon feels by the end of the week. Steadier energy and fewer
headaches are the common reports, and the habit costs nothing to keep. If you
train, have another glass around your session, and more on hot days when you
sweat more than usual. Keep a glass or bottle where you can see it.`

func TestScreenPassesAPlainPost(t *testing.T) {
	t.Parallel()

	if problems := Screen(goodTitle, goodBody); len(problems) > 0 {
		t.Fatalf("a plain post was refused: %v", problems)
	}
}

func TestScreenRefusesClaimsAndFiller(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"dosage":              "Take 500 mg of magnesium at night.",
		"calorie target":      "Aim for 1200 calories a day.",
		"supplement":          "A supplement can fill the gap.",
		"cure":                "Walking cures insomnia.",
		"detox":               "A green juice helps your body detox.",
		"fat burning":         "Cold showers burn fat.",
		"fasting":             "Try fasting until noon.",
		"weight loss promise": "You could lose 5 kg this month.",
		"emoji":               "Drink up 💧",
	}
	for want, sentence := range cases {
		problems := Screen(goodTitle, goodBody+"\n\n"+sentence)
		if !slices.Contains(problems, want) {
			t.Errorf("%q: got %v, want %q", sentence, problems, want)
		}
	}
}

func TestScreenRefusesTheWrongShape(t *testing.T) {
	t.Parallel()

	if p := Screen(goodTitle, "Too short."); !slices.Contains(p, "too short") {
		t.Errorf("short body: %v", p)
	}
	if p := Screen(goodTitle, strings.Repeat("word ", maxWords+1)); !slices.Contains(p, "too long") {
		t.Errorf("long body: %v", p)
	}
	if p := Screen("", goodBody); !slices.Contains(p, "title missing or too long") {
		t.Errorf("no title: %v", p)
	}
	if p := Screen(goodTitle, goodBody+"\n1. drink\n2. eat"); !slices.Contains(p, "numbered list") {
		t.Errorf("list: %v", p)
	}
}
